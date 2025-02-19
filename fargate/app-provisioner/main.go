package main

import (
	"context"
	"fmt"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/pennsieve/app-deploy-service/app-provisioner/provisioner"
	"github.com/pennsieve/app-deploy-service/app-provisioner/provisioner/pusher_config"
	"github.com/pennsieve/app-deploy-service/app-provisioner/provisioner/status"
	"log"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	"github.com/aws/aws-sdk-go-v2/service/ecs/types"
	awsProvisioner "github.com/pennsieve/app-deploy-service/app-provisioner/provisioner/aws"
	"github.com/pennsieve/app-deploy-service/app-provisioner/provisioner/parser"
	"github.com/pennsieve/app-deploy-service/app-provisioner/provisioner/runner"
	"github.com/pennsieve/app-deploy-service/app-provisioner/provisioner/store_dynamodb"
	"github.com/pennsieve/app-deploy-service/app-provisioner/provisioner/utils"
)

func main() {
	log.Println("Running app Provisioner")
	ctx := context.Background()

	applicationUuid := os.Getenv("APPLICATION_UUID")
	action := os.Getenv("ACTION")
	accountId := os.Getenv("ACCOUNT_ID")
	env := os.Getenv("ENV")
	sourceUrl := os.Getenv("SOURCE_URL")
	destinationUrl := os.Getenv("DESTINATION_URL")
	storageId := os.Getenv("COMPUTE_NODE_EFS_ID")
	computeNodeUuid := os.Getenv("COMPUTE_NODE_UUID")

	applicationsTable := os.Getenv("APPLICATIONS_TABLE")

	// Initializing environment
	cfg, err := config.LoadDefaultConfig(context.Background())
	if err != nil {
		log.Fatalf("LoadDefaultConfig: %v\n", err)
	}

	appProvisioner := awsProvisioner.NewAWSProvisioner(cfg,
		accountId, action, env, utils.ExtractGitUrl(sourceUrl), storageId, utils.AppSlug(sourceUrl, computeNodeUuid))
	dynamoDBClient := dynamodb.NewFromConfig(cfg)
	applicationsStore := store_dynamodb.NewApplicationDatabaseStore(dynamoDBClient, applicationsTable)
	statusManager := status.NewManager(applicationsStore, applicationUuid)

	deploymentsTable := os.Getenv(provisioner.DeploymentsTableNameKey)
	deploymentsStore := store_dynamodb.NewDeploymentsStore(dynamoDBClient, deploymentsTable)

	// deploymentId will only be present if this is not a DELETE. DELETE does not
	// generate a Deployment record that needs to be updated.
	var deploymentId string
	if action == "CREATE" || action == "DEPLOY" {
		deploymentId = os.Getenv(provisioner.DeploymentIdKey)
		statusManager = statusManager.WithDeployment(deploymentsStore, deploymentId)
	}

	// use pusher if we can get the config
	if pusherConfig, err := pusher_config.Get(ctx, ssm.NewFromConfig(cfg)); err != nil {
		log.Printf("warning: unable to configure Pusher: %s\n", err.Error())
	} else {
		statusManager = statusManager.WithPusher(pusherConfig)
	}

	// POST provisioning actions
	switch action {
	case "CREATE":
		ecsClient := ecs.NewFromConfig(cfg)
		if err := Create(ctx, applicationUuid, deploymentId, sourceUrl, appProvisioner, ecsClient, statusManager); err != nil {
			statusManager.SetErrorStatus(ctx, err)
			log.Fatal(err)
		}
	case "DELETE":
		if err := Delete(ctx, applicationUuid, appProvisioner, applicationsStore, deploymentsStore); err != nil {
			statusManager.UpdateApplicationStatus(ctx, err.Error(), true)
			log.Fatal(err)
		}
	case "DEPLOY":
		// Build and deploy
		ecsClient := ecs.NewFromConfig(cfg)
		if err := Redeploy(ctx, applicationUuid, deploymentId, sourceUrl, destinationUrl, appProvisioner, ecsClient, statusManager); err != nil {
			statusManager.SetErrorStatus(ctx, err)
			log.Fatal(err)
		}
	default:
		unknownActionStatus := fmt.Sprintf("error: unknown provision action: %s", action)
		statusManager.UpdateApplicationStatus(ctx, unknownActionStatus, true)
		log.Fatalf("action not supported: %s", action)
	}

	log.Println("provisioning complete")
}

func Create(ctx context.Context, applicationUuid string, deploymentId string, sourceUrl string, appProvisioner provisioner.Provisioner, ecsClient *ecs.Client, statusManager *status.Manager) error {
	if err := appProvisioner.Create(ctx); err != nil {
		return fmt.Errorf("error creating infrastructure: %w", err)
	}
	// parse output file created after infrastructure creation
	parser := parser.NewOutputParser("/usr/src/app/terraform/infrastructure/outputs.json")
	outputs, err := parser.Run(ctx)
	if err != nil {
		return fmt.Errorf("error running output parser: %w", err)
	}

	// update application record
	store_application := store_dynamodb.Application{
		ApplicationId:            outputs.AppTaskDefn.Value,
		ApplicationContainerName: outputs.AppContainerName.Value,
		DestinationUrl:           outputs.AppEcrUrl.Value,
		Status:                   "deploying",
	}
	err = statusManager.ApplicationCreateUpdate(ctx, store_application)
	if err != nil {
		return fmt.Errorf("error updating application record: %w", err)
	}

	// Build and deploy
	log.Println("Initiating new Deployment Fargate Task: CREATE")
	if err := Deploy(ctx, applicationUuid, deploymentId, sourceUrl, store_application.DestinationUrl, appProvisioner, ecsClient); err != nil {
		return err
	}

	return nil
}

func Redeploy(ctx context.Context, applicationUuid string, deploymentId string, sourceUrl string, destinationUrl string, appProvisioner provisioner.Provisioner, ecsClient *ecs.Client, statusManager *status.Manager) error {
	log.Println("Initiating new Deployment Fargate Task: DEPLOY")
	statusManager.UpdateApplicationStatus(ctx, "re-deploying", false)
	if err := Deploy(ctx, applicationUuid, deploymentId, sourceUrl, destinationUrl, appProvisioner, ecsClient); err != nil {
		return err
	}
	return nil
}

func Deploy(ctx context.Context, applicationUuid string, deploymentId string, sourceUrl string, destinationUrl string, appProvisioner provisioner.Provisioner, ecsClient *ecs.Client) error {
	creds, err := appProvisioner.AssumeRole(ctx)
	if err != nil {
		return fmt.Errorf("error assuming role: %w", err)
	}

	accessKeyId := "AWS_ACCESS_KEY_ID"
	accessKeyIdValue := creds.AccessKeyID
	secretAccessKey := "AWS_SECRET_ACCESS_KEY"
	secretAccessKeyValue := creds.SecretAccessKey
	sessionToken := "AWS_SESSION_TOKEN"
	sessionTokenValue := creds.SessionToken

	TaskDefinitionArn := os.Getenv("DEPLOYER_TASK_DEF_ARN")
	subIdStr := os.Getenv("SUBNET_IDS")
	SubNetIds := strings.Split(subIdStr, ",")
	cluster := os.Getenv("CLUSTER_ARN")
	SecurityGroup := os.Getenv("SECURITY_GROUP")
	TaskDefContainerName := os.Getenv("DEPLOYER_TASK_DEF_CONTAINER_NAME")

	runTaskIn := &ecs.RunTaskInput{
		TaskDefinition: aws.String(TaskDefinitionArn),
		Cluster:        aws.String(cluster),
		NetworkConfiguration: &types.NetworkConfiguration{
			AwsvpcConfiguration: &types.AwsVpcConfiguration{
				Subnets:        SubNetIds,
				SecurityGroups: []string{SecurityGroup},
				AssignPublicIp: types.AssignPublicIpEnabled,
			},
		},
		Overrides: &types.TaskOverride{
			ContainerOverrides: []types.ContainerOverride{
				{
					Name:    &TaskDefContainerName,
					Command: []string{"--context", sourceUrl, "--destination", destinationUrl, "--force"},
					Environment: []types.KeyValuePair{
						{
							Name:  &accessKeyId,
							Value: &accessKeyIdValue,
						},
						{
							Name:  &sessionToken,
							Value: &sessionTokenValue,
						},
						{
							Name:  &secretAccessKey,
							Value: &secretAccessKeyValue,
						},
					},
				},
			},
		},
		LaunchType: types.LaunchTypeFargate,
		Tags: []types.Tag{
			{Key: aws.String(provisioner.DeploymentIdTag), Value: aws.String(deploymentId)},
			{Key: aws.String(provisioner.ApplicationIdTag), Value: aws.String(applicationUuid)},
		},
	}

	taskRunner := runner.NewECSTaskRunner(ecsClient, runTaskIn)
	runTaskOut, err := taskRunner.Run(ctx)
	if err != nil {
		return fmt.Errorf("error running deployment task: %w", err)
	}
	if err := runner.GetRunFailures(runTaskOut); err != nil {
		return fmt.Errorf("error: run failures: %w", err)
	}
	return nil
}

func Delete(ctx context.Context, applicationUuid string, appProvisioner provisioner.Provisioner, applicationsStore store_dynamodb.DynamoDBStore, deploymentsStore *store_dynamodb.DeploymentsStore) error {
	log.Println("Deleting", applicationUuid)

	if err := appProvisioner.Delete(ctx); err != nil {
		return fmt.Errorf("error deleting infrastructure: :%w", err)
	}

	if err := applicationsStore.Delete(ctx, applicationUuid); err != nil {
		return fmt.Errorf("error deleting application from store: %w", err)
	}
	if err := deploymentsStore.DeleteApplicationDeployments(ctx, applicationUuid); err != nil {
		log.Printf("warning: error deleting deployment records for deleted application %s: %v\n", applicationUuid, err)
	}
	return nil
}

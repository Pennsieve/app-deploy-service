package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/pennsieve/app-deploy-service/app-provisioner/provisioner"
	"github.com/pennsieve/app-deploy-service/app-provisioner/provisioner/pusher_config"
	"github.com/pennsieve/app-deploy-service/app-provisioner/provisioner/status"

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
	accountUuid := os.Getenv("ACCOUNT_UUID")
	env := os.Getenv("ENV")
	sourceUrl := os.Getenv("SOURCE_URL")
	destinationUrl := os.Getenv("DESTINATION_URL")
	storageId := os.Getenv("COMPUTE_NODE_EFS_ID")
	computeNodeUuid := os.Getenv("COMPUTE_NODE_UUID")
	runOnGPU := os.Getenv("RUN_ON_GPU") == "true"

	applicationsTable := os.Getenv("APPLICATIONS_TABLE")
	accountsTable := os.Getenv("ACCOUNTS_TABLE")

	var tag string
	tag = os.Getenv("SOURCE_TAG")
	if tag == "" {
		tag = "latest"
	}

	// Initializing environment
	cfg, err := config.LoadDefaultConfig(context.Background())
	if err != nil {
		log.Fatalf("LoadDefaultConfig: %v\n", err)
	}

	dynamoDBClient := dynamodb.NewFromConfig(cfg)

	// Look up the account to get the role name (not needed for appstore deployments)
	var roleName string
	if action != "ADD_TO_APPSTORE" {
		accountStore := store_dynamodb.NewAccountStore(dynamoDBClient, accountsTable)
		account, err := accountStore.GetById(ctx, accountUuid)
		if err != nil {
			log.Fatalf("error looking up account %s: %v", accountUuid, err)
		}
		log.Printf("resolved roleName %q for account %s", account.RoleName, accountUuid)
		roleName = account.RoleName
	}

	appProvisioner := awsProvisioner.NewAWSProvisioner(cfg,
		accountId, action, env, utils.ExtractGitUrl(sourceUrl), storageId, utils.AppSlug(sourceUrl, computeNodeUuid), runOnGPU, roleName)
	applicationsStore := store_dynamodb.NewApplicationDatabaseStore(dynamoDBClient, applicationsTable)
	statusManager := status.NewManager(applicationsStore, applicationUuid)

	// deploymentId will only be present if this is not a DELETE or ADD_TO_APPSTORE.
	// ADD_TO_APPSTORE handles its own status manager setup.
	var deploymentId string
	if action == "CREATE" || action == "DEPLOY" {
		deploymentsTable := os.Getenv(provisioner.DeploymentsTableNameKey)
		deploymentId = os.Getenv(provisioner.DeploymentIdKey)
		deploymentsStore := store_dynamodb.NewDeploymentsStore(dynamoDBClient, deploymentsTable)
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
		if err := Delete(ctx, applicationUuid, appProvisioner, applicationsStore); err != nil {
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
	case "ADD_TO_APPSTORE":
		// APPLICATIONS_TABLE points to the versions table for appstore deployments
		versionStore := store_dynamodb.NewAppStoreVersionDatabaseStore(dynamoDBClient, applicationsTable)
		appStoreStatusManager := status.NewAppStoreManager(versionStore, applicationUuid)
		deploymentsTable := os.Getenv(provisioner.DeploymentsTableNameKey)
		appStoreDeploymentId := os.Getenv(provisioner.DeploymentIdKey)
		deploymentsStore := store_dynamodb.NewDeploymentsStore(dynamoDBClient, deploymentsTable)
		appStoreStatusManager = appStoreStatusManager.WithDeployment(deploymentsStore, appStoreDeploymentId)
		if pusherConfig, err := pusher_config.Get(ctx, ssm.NewFromConfig(cfg)); err != nil {
			log.Printf("warning: unable to configure Pusher: %s\n", err.Error())
		} else {
			appStoreStatusManager = appStoreStatusManager.WithPusher(pusherConfig)
		}

		ecsClient := ecs.NewFromConfig(cfg)
		authToken := os.Getenv("AUTH_TOKEN")
		err := AddToAppstore(ctx, applicationUuid, appStoreDeploymentId, sourceUrl, tag, authToken, appProvisioner, ecsClient, appStoreStatusManager, versionStore)
		if err != nil {
			appStoreStatusManager.SetErrorStatus(ctx, err)
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

func AddToAppstore(ctx context.Context, applicationUuid string, deploymentId string, sourceUrl string, tag string, authToken string, appProvisioner provisioner.Provisioner, ecsClient *ecs.Client, statusManager *status.Manager, versionStore store_dynamodb.AppStoreVersionDBStore) error {
	// Get the pre-existing private ECR URL from environment variable
	ecrRepoUrl := os.Getenv("APPSTORE_PRIVATE_ECR_URL")
	if ecrRepoUrl == "" {
		return fmt.Errorf("APPSTORE_PRIVATE_ECR_URL environment variable is not set")
	}

	// Generate unique tag using source URL hash: {hash}-{source_tag}
	// This ensures each source gets unique tags in the shared ECR repo
	sourceUrlHash := utils.GenerateHash(sourceUrl)
	uniqueTag := fmt.Sprintf("%d-%s", sourceUrlHash, tag)
	destinationUrl := fmt.Sprintf("%s:%s", ecrRepoUrl, uniqueTag)

	log.Printf("Using private ECR repository with unique tag: %s", destinationUrl)

	// Update the version record with destination URL
	if err := versionStore.UpdateDestinationUrl(ctx, applicationUuid, destinationUrl, "deploying"); err != nil {
		return fmt.Errorf("error updating appstore version with destination URL: %w", err)
	}
	statusManager.UpdateApplicationStatus(ctx, "deploying", false)

	// Build and push
	log.Printf("Initiating new Deployment Fargate Task: ADD_TO_APPSTORE - sourceUrl: %s, tag: %s, destinationUrl: %s", sourceUrl, tag, destinationUrl)
	applicationsTable := os.Getenv("APPLICATIONS_TABLE")
	if err := PrivateDeploy(ctx, applicationUuid, deploymentId, sourceUrl, tag, destinationUrl, authToken, applicationsTable, appProvisioner, ecsClient); err != nil {
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

func Delete(ctx context.Context, applicationUuid string, appProvisioner provisioner.Provisioner, applicationsStore store_dynamodb.DynamoDBStore) error {
	log.Println("Deleting", applicationUuid)

	if err := appProvisioner.Delete(ctx); err != nil {
		return fmt.Errorf("error deleting infrastructure: :%w", err)
	}

	if err := applicationsStore.Delete(ctx, applicationUuid); err != nil {
		return fmt.Errorf("error deleting application from store: %w", err)
	}
	return nil
}

func PublicDeploy(ctx context.Context, applicationUuid string, deploymentId string, sourceUrl string, tag string, destinationUrl string, appProvisioner provisioner.Provisioner, ecsClient *ecs.Client) error {
	creds, err := appProvisioner.GetProvisionerCreds(ctx)
	if err != nil {
		return fmt.Errorf("error retrieving credentials: %w", err)
	}

	deploymentSourceUrl, err := utils.DetermineSourceURL(sourceUrl, tag)
	if err != nil {
		return fmt.Errorf("error determining sourceUrl variable for deployment: %w", err)
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
					Command: []string{"--context", deploymentSourceUrl, "--destination", fmt.Sprintf("%s:%s", destinationUrl, tag), "--force"},
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

func PrivateDeploy(ctx context.Context, applicationUuid string, deploymentId string, sourceUrl string, tag string, destinationUrl string, authToken string, applicationsTable string, appProvisioner provisioner.Provisioner, ecsClient *ecs.Client) error {
	creds, err := appProvisioner.GetProvisionerCreds(ctx)
	if err != nil {
		return fmt.Errorf("error retrieving credentials: %w", err)
	}

	deploymentSourceUrl, err := utils.DetermineSourceURL(sourceUrl, tag)
	if err != nil {
		return fmt.Errorf("error determining sourceUrl variable for deployment: %w", err)
	}

	// destinationUrl already contains the full image reference with unique tag
	// Format: {ecr_repo}:{source_hash}-{source_tag}

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

	envVars := []types.KeyValuePair{
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
	}

	// Add GIT_TOKEN for kaniko to authenticate with private GitHub repos
	if authToken != "" {
		envVars = append(envVars, types.KeyValuePair{
			Name:  aws.String("GIT_TOKEN"),
			Value: aws.String(authToken),
		})
	}

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
					Name:        &TaskDefContainerName,
					Command:     []string{"--context", deploymentSourceUrl, "--destination", destinationUrl, "--force"},
					Environment: envVars,
				},
			},
		},
		LaunchType: types.LaunchTypeFargate,
		Tags: []types.Tag{
			{Key: aws.String(provisioner.DeploymentIdTag), Value: aws.String(deploymentId)},
			{Key: aws.String(provisioner.ApplicationIdTag), Value: aws.String(applicationUuid)},
		},
	}

	// Propagate ApplicationsTable tag so the status lambda updates the correct table
	if applicationsTable != "" {
		runTaskIn.Tags = append(runTaskIn.Tags, types.Tag{
			Key:   aws.String(provisioner.ApplicationsTableTag),
			Value: aws.String(applicationsTable),
		})
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

package main

import (
	"context"
	"log"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	"github.com/aws/aws-sdk-go-v2/service/ecs/types"
	"github.com/aws/aws-sdk-go/aws"
	awsProvisioner "github.com/pennsieve/app-deploy-service/app-provisioner/provisioner/aws"
	"github.com/pennsieve/app-deploy-service/app-provisioner/provisioner/parser"
	"github.com/pennsieve/app-deploy-service/app-provisioner/provisioner/runner"
	"github.com/pennsieve/app-deploy-service/app-provisioner/provisioner/store_dynamodb"
	"github.com/pennsieve/app-deploy-service/app-provisioner/provisioner/utils"
)

func main() {
	log.Println("Running app provisioner")
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

	provisioner := awsProvisioner.NewAWSProvisioner(cfg,
		accountId, action, env, utils.ExtractGitUrl(sourceUrl), storageId, utils.AppSlug(sourceUrl, computeNodeUuid))

	if action != "DEPLOY" {
		err = provisioner.Run(ctx)
		if err != nil {
			log.Fatal("error running provisioner", err.Error())
		}
	}

	// POST provisioning actions
	switch action {
	case "CREATE":
		// parse output file created after infrastructure creation
		parser := parser.NewOutputParser("/usr/src/app/terraform/infrastructure/outputs.json")
		outputs, err := parser.Run(ctx)
		if err != nil {
			log.Fatal("error running output parser", err.Error())
		}

		// update application record
		dynamoDBClient := dynamodb.NewFromConfig(cfg)
		applicationsStore := store_dynamodb.NewApplicationDatabaseStore(dynamoDBClient, applicationsTable)

		store_application := store_dynamodb.Application{
			ApplicationId:            outputs.AppTaskDefn.Value,
			ApplicationContainerName: outputs.AppContainerName.Value,
			DestinationUrl:           outputs.AppEcrUrl.Value,
			Status:                   "deploying",
		}
		err = applicationsStore.Update(ctx, store_application, applicationUuid)
		if err != nil {
			log.Fatal(err.Error())
		}

		// Build and deploy
		escClient := ecs.NewFromConfig(cfg)
		log.Println("Initiating new Deployment Fargate Task:", action)
		creds, err := provisioner.AssumeRole(ctx)
		if err != nil {
			log.Fatal(err.Error())
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
						Command: []string{"--context", sourceUrl, "--destination", store_application.DestinationUrl, "--force"},
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
		}
		runner := runner.NewECSTaskRunner(escClient, runTaskIn)
		if err := runner.Run(ctx); err != nil {
			log.Fatal(err)
		}

	case "DELETE":
		log.Println("Deleting", applicationUuid)
		dynamoDBClient := dynamodb.NewFromConfig(cfg)
		applicationsStore := store_dynamodb.NewApplicationDatabaseStore(dynamoDBClient, applicationsTable)

		err = applicationsStore.Delete(ctx, applicationUuid)
		if err != nil {
			log.Fatal(err.Error())
		}
	case "DEPLOY":
		// Build and deploy
		escClient := ecs.NewFromConfig(cfg)
		dynamoDBClient := dynamodb.NewFromConfig(cfg)
		log.Println("Initiating new Deployment Fargate Task:", action)
		creds, err := provisioner.AssumeRole(ctx)
		if err != nil {
			log.Fatal(err.Error())
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
		}
		applicationsStore := store_dynamodb.NewApplicationDatabaseStore(dynamoDBClient, applicationsTable)
		if err := applicationsStore.UpdateStatus(ctx, "re-deploying", applicationUuid); err != nil {
			log.Fatalf("error updating status of application %s to `re-deploying`: %v", applicationUuid, err)
		}
		runner := runner.NewECSTaskRunner(escClient, runTaskIn)
		if err := runner.Run(ctx); err != nil {
			log.Fatal(err)
		}

	default:
		log.Fatalf("action not supported: %s", action)
	}

	log.Println("provisioning complete")
}

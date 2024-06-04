package main

import (
	"context"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	"github.com/aws/aws-sdk-go-v2/service/ecs/types"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/google/uuid"
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
	applicationType := os.Getenv("APPLICATION_TYPE")

	action := os.Getenv("ACTION")

	accountUuid := os.Getenv("ACCOUNT_UUID")
	accountId := os.Getenv("ACCOUNT_ID")
	accountType := os.Getenv("ACCOUNT_TYPE")

	organizationId := os.Getenv("ORG_ID")
	userId := os.Getenv("USER_ID")
	env := os.Getenv("ENV")

	applicationName := os.Getenv("APPLICATION_NAME")
	applicationDescription := os.Getenv("APPLICATION_DESCRIPTION")

	sourceUrl := os.Getenv("SOURCE_URL")
	sourceType := os.Getenv("SOURCE_TYPE")

	destinationUrl := os.Getenv("DESTINATION_URL")

	computeNodeUuid := os.Getenv("COMPUTE_NODE_UUID")
	computeNodeEfsId := os.Getenv("COMPUTE_NODE_EFS_ID")

	applicationsTable := os.Getenv("APPLICATIONS_TABLE")

	appCPU := os.Getenv("APP_CPU")
	appMemory := os.Getenv("APP_MEMORY")

	// Initializing environment
	cfg, err := config.LoadDefaultConfig(context.Background())
	if err != nil {
		log.Fatalf("LoadDefaultConfig: %v\n", err)
	}

	provisioner := awsProvisioner.NewAWSProvisioner(iam.NewFromConfig(cfg), sts.NewFromConfig(cfg),
		accountId, action, env, utils.ExtractGitUrl(sourceUrl), computeNodeEfsId, utils.ExtractRepoName(sourceUrl))

	if action != "DEPLOY" {
		err = provisioner.Run(ctx)
		if err != nil {
			log.Fatal("error running provisioner", err.Error())
		}
	}

	// POST provisioning actions
	switch action {
	case "CREATE":
		policy, err := provisioner.GetPolicy(context.Background())
		if err != nil {
			log.Fatalf("get policy error: %v\n", err)
		}

		if policy == nil {
			log.Printf("no inline policy exists for account: %s, creating ...", accountId)
			err = provisioner.CreatePolicy(context.Background())
			if err != nil {
				log.Fatalf("create policy error: %v\n", err)
			}
		}

		// parse output file created after infrastructure creation
		parser := parser.NewOutputParser("/usr/src/app/terraform/infrastructure/outputs.json")
		outputs, err := parser.Run(ctx)
		if err != nil {
			log.Fatal("error running output parser", err.Error())
		}

		// persist to dynamodb
		dynamoDBClient := dynamodb.NewFromConfig(cfg)
		applicationsStore := store_dynamodb.NewApplicationDatabaseStore(dynamoDBClient, applicationsTable)

		applications, err := applicationsStore.Get(ctx, computeNodeUuid, sourceUrl)
		if err != nil {
			log.Fatal(err.Error())
		}
		if len(applications) > 0 {
			log.Fatalf("application with computeNodeUuid: %s already exists", computeNodeUuid)
		}

		appCPUInt, err := strconv.Atoi(appCPU)
		if err != nil {
			log.Fatal(err.Error())
		}
		appMemoryInt, err := strconv.Atoi(appMemory)
		if err != nil {
			log.Fatal(err.Error())
		}

		destinationUrl = outputs.AppEcrUrl.Value
		id := uuid.New()
		applicationId := id.String()
		store_applications := store_dynamodb.Application{
			Uuid:                     applicationId,
			Name:                     applicationName,
			Description:              applicationDescription,
			ApplicationType:          applicationType,
			ApplicationId:            outputs.AppTaskDefn.Value,
			ApplicationContainerName: outputs.AppContainerName.Value,
			AccountUuid:              accountUuid,
			AccountId:                accountId,
			AccountType:              accountType,
			ComputeNodeUuid:          computeNodeUuid,
			ComputeNodeEfsId:         computeNodeEfsId,
			SourceType:               sourceType,
			SourceUrl:                sourceUrl,
			DestinationType:          "ecr",
			DestinationUrl:           destinationUrl,
			CPU:                      appCPUInt,
			Memory:                   appMemoryInt,
			Env:                      env,
			OrganizationId:           organizationId,
			UserId:                   userId,
			CreatedAt:                time.Now().UTC().String(),
		}
		err = applicationsStore.Insert(ctx, store_applications)
		if err != nil {
			log.Fatal(err.Error())
		}

		// Build and deploy
		escClient := ecs.NewFromConfig(cfg)
		log.Println("Initiating new Deployment Fargate Task.")
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
		log.Println("Initiating new Deployment Fargate Task.")
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
		runner := runner.NewECSTaskRunner(escClient, runTaskIn)
		if err := runner.Run(ctx); err != nil {
			log.Fatal(err)

		}

	default:
		log.Fatalf("action not supported: %s", action)
	}

	log.Println("provisioning complete")
}

package main

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/google/uuid"
	aws "github.com/pennsieve/app-deploy-service/app-provisioner/provisioner/aws"
	"github.com/pennsieve/app-deploy-service/app-provisioner/provisioner/parser"
	"github.com/pennsieve/app-deploy-service/app-provisioner/provisioner/store_dynamodb"
	"github.com/pennsieve/app-deploy-service/app-provisioner/provisioner/utils"
)

func main() {
	log.Println("Running app provisioner")
	ctx := context.Background()

	applicationId := os.Getenv("APPLICATION_ID")
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

	computeNodeUuid := os.Getenv("COMPUTE_NODE_UUID")
	computeNodeEfsId := os.Getenv("COMPUTE_NODE_EFS_ID")

	applicationsTable := os.Getenv("APPLICATIONS_TABLE")

	// Initializing environment
	cfg, err := config.LoadDefaultConfig(context.Background())
	if err != nil {
		log.Fatalf("LoadDefaultConfig: %v\n", err)
	}

	provisioner := aws.NewAWSProvisioner(iam.NewFromConfig(cfg), sts.NewFromConfig(cfg),
		accountId, action, env, utils.ExtractGitUrl(sourceUrl), computeNodeEfsId, utils.ExtractRepoName(sourceUrl))
	err = provisioner.Run(ctx)
	if err != nil {
		log.Fatal("error running provisioner", err.Error())
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

		// persist to dynamodb
		dynamoDBClient := dynamodb.NewFromConfig(cfg)
		applicationsStore := store_dynamodb.NewApplicationDatabaseStore(dynamoDBClient, applicationsTable)

		applications, err := applicationsStore.Get(ctx, computeNodeUuid, sourceUrl)
		if err != nil {
			log.Fatal(err.Error())
		}
		if len(applications) > 0 {
			log.Fatalf("application with env: %s already exists", applications[0].Env)
		}

		id := uuid.New()
		applicationId := id.String()
		store_applications := store_dynamodb.Application{
			Uuid:             applicationId,
			Name:             applicationName,
			Description:      applicationDescription,
			ApplicationType:  applicationType,
			AccountUuid:      accountUuid,
			AccountId:        accountId,
			AccountType:      accountType,
			ComputeNodeUuid:  computeNodeUuid,
			ComputeNodeEfsId: computeNodeEfsId,
			SourceType:       sourceType,
			SourceUrl:        sourceUrl,
			DestinationType:  "ecr",
			DestinationUrl:   outputs.AppEcrUrl.Value,
			Env:              env,
			OrganizationId:   organizationId,
			UserId:           userId,
			CreatedAt:        time.Now().UTC().String(),
		}
		err = applicationsStore.Insert(ctx, store_applications)
		if err != nil {
			log.Fatal(err.Error())
		}
	case "DELETE":
		log.Println("Deleting", applicationId)
		dynamoDBClient := dynamodb.NewFromConfig(cfg)
		applicationsStore := store_dynamodb.NewApplicationDatabaseStore(dynamoDBClient, applicationsTable)

		err = applicationsStore.Delete(ctx, applicationId)
		if err != nil {
			log.Fatal(err.Error())
		}

	default:
		log.Fatalf("action not supported: %s", action)
	}

	log.Println("provisioning complete")
}

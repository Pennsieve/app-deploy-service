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
)

func main() {
	log.Println("Running app provisioner")
	ctx := context.Background()

	applicationId := os.Getenv("APPLICATION_ID")
	action := os.Getenv("ACTION")

	accountUuid := os.Getenv("ACCOUNT_UUID")
	accountId := os.Getenv("ACCOUNT_ID")
	organizationId := os.Getenv("ORG_ID")
	userId := os.Getenv("USER_ID")
	env := os.Getenv("ENV")
	applicationName := os.Getenv("NODE_NAME")
	applicationDescription := os.Getenv("NODE_DESCRIPTION")

	applicationsTable := os.Getenv("APPLICATIONS_TABLE")

	// Initializing environment
	cfg, err := config.LoadDefaultConfig(context.Background())
	if err != nil {
		log.Fatalf("LoadDefaultConfig: %v\n", err)
	}

	provisioner := aws.NewAWSProvisioner(iam.NewFromConfig(cfg), sts.NewFromConfig(cfg),
		accountId, action, env)
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

		applications, err := applicationsStore.Get(ctx, accountUuid, env)
		if err != nil {
			log.Fatal(err.Error())
		}
		if len(applications) > 1 {
			log.Fatal("expected only one application entry")
		}
		if len(applications) == 1 {
			log.Fatalf("application with env: %s already exists",
				applications[0].Env)

		}

		id := uuid.New()
		applicationId := id.String()
		store_nodes := store_dynamodb.Application{
			Uuid:           applicationId,
			Name:           applicationName,
			Description:    applicationDescription,
			AppEcrUrl:      outputs.AppEcrUrl.Value,
			Env:            env,
			OrganizationId: organizationId,
			UserId:         userId,
			CreatedAt:      time.Now().UTC().String(),
		}
		err = applicationsStore.Insert(ctx, store_nodes)
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

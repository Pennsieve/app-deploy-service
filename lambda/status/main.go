package main

import (
	"context"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	"github.com/pennsieve/app-deploy-service/status/handler"
	"github.com/pennsieve/app-deploy-service/status/logging"
	"log/slog"
	"os"
)

// This Lambda listens for ECS state change events and logs them to DynamoDB
var stateChangeHandler *handler.DeployTaskStateChangeHandler

func init() {
	awsConfig, err := config.LoadDefaultConfig(context.Background())
	if err != nil {
		logging.Default.Error("error loading AWS config", slog.Any("error", err))
		os.Exit(1)
	}
	applicationsTable := os.Getenv(handler.ApplicationsTableEnvVar)
	if len(applicationsTable) == 0 {
		logging.Default.Error("empty or missing env var value", slog.String("missing", handler.ApplicationsTableEnvVar))
	}
	deploymentsTable := os.Getenv(handler.DeploymentsTableEnvVar)
	if len(deploymentsTable) == 0 {
		logging.Default.Error("empty or missing env var value", slog.String("missing", handler.DeploymentsTableEnvVar))
	}
	stateChangeHandler = handler.NewDeployTaskStateChangeHandler(
		ecs.NewFromConfig(awsConfig),
		dynamodb.NewFromConfig(awsConfig),
		applicationsTable,
		deploymentsTable)
}

func main() {
	lambda.Start(stateChangeHandler.Handle)
}

package main

import (
	"context"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/pennsieve/app-deploy-service/status/handler"
	"github.com/pennsieve/app-deploy-service/status/logging"
	"github.com/pusher/pusher-http-go/v5"
	"log/slog"
	"os"
)

// This Lambda listens for ECS state change events and logs them to DynamoDB
var stateChangeHandler *handler.DeployTaskStateChangeHandler

func init() {
	ctx := context.Background()
	awsConfig, err := config.LoadDefaultConfig(ctx)
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

	if pusherConfig, err := handler.GetPusherConfig(ctx, ssm.NewFromConfig(awsConfig)); err != nil {
		logging.Default.Warn("unable to get pusher config", slog.Any("error", err))
	} else {
		stateChangeHandler = stateChangeHandler.WithPusher(&pusher.Client{
			AppID:   pusherConfig.AppId,
			Key:     pusherConfig.Key,
			Secret:  pusherConfig.Secret,
			Cluster: pusherConfig.Cluster,
			Secure:  true,
		})
	}

}

func main() {
	lambda.Start(stateChangeHandler.Handle)
}

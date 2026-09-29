// check-app-access is invoked synchronously by the realtime events authorizer to decide whether a user may
// subscribe to /applications/<appUuid>. See access.CheckRequest / access.CheckResponse for the contract.
package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/pennsieve/app-deploy-service/service/access"
	"github.com/pennsieve/app-deploy-service/service/logging"
	"github.com/pennsieve/app-deploy-service/service/store_dynamodb"
)

const (
	appstoreApplicationsTableEnvVar = "APPSTORE_APPLICATIONS_TABLE"
	applicationsTableEnvVar         = "APPLICATIONS_TABLE"
	appstoreVersionsTableEnvVar     = "APPSTORE_VERSIONS_TABLE"
	appAccessTableEnvVar            = "APP_ACCESS_TABLE"
)

var checker *access.Checker

func init() {
	cfg, err := config.LoadDefaultConfig(context.Background())
	if err != nil {
		logging.Default.Error("error loading AWS config", slog.Any("error", err))
		os.Exit(1)
	}
	appsTable := requireEnv(appstoreApplicationsTableEnvVar)
	applicationsTable := requireEnv(applicationsTableEnvVar)
	versionsTable := requireEnv(appstoreVersionsTableEnvVar)
	accessTable := requireEnv(appAccessTableEnvVar)
	client := dynamodb.NewFromConfig(cfg)
	checker = access.NewChecker(
		store_dynamodb.NewAppStoreDatabaseStore(client, appsTable),
		store_dynamodb.NewApplicationDatabaseStore(client, applicationsTable),
		store_dynamodb.NewAppStoreVersionDatabaseStore(client, versionsTable),
		store_dynamodb.NewAppAccessDatabaseStore(client, accessTable))
}

func requireEnv(key string) string {
	value := os.Getenv(key)
	if value == "" {
		logging.Default.Error("empty or missing env var value", slog.String("missing", key))
		os.Exit(1)
	}
	return value
}

func handle(ctx context.Context, req access.CheckRequest) (access.CheckResponse, error) {
	resp, err := checker.Check(ctx, req)
	if err != nil {
		logging.Default.Error("error checking app access",
			slog.String("appUuid", req.AppUuid),
			slog.String("userNodeId", req.UserNodeId),
			slog.Any("error", err))
		return resp, err
	}
	logging.Default.Info("checked app access",
		slog.String("appUuid", req.AppUuid),
		slog.String("userNodeId", req.UserNodeId),
		slog.Bool("hasAccess", resp.HasAccess))
	return resp, nil
}

func main() {
	lambda.Start(handle)
}

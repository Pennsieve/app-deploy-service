package handler

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/pennsieve/app-deploy-service/service/mappers"
	"github.com/pennsieve/app-deploy-service/service/store_dynamodb"
)

func GetAppstoreApplicationsHandler(ctx context.Context, request events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	handlerName := "GetAppstoreApplicationsHandler"
	queryParams := request.QueryStringParameters
	log.Println(queryParams)

	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		log.Println(err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       handlerError(handlerName, ErrConfig),
		}, nil
	}
	dynamoDBClient := dynamodb.NewFromConfig(cfg)
	applicationsTable := os.Getenv("APPLICATIONS_TABLE")
	deploymentsTable := os.Getenv(deploymentsTableNameKey)

	dynamo_store := store_dynamodb.NewApplicationDatabaseStore(dynamoDBClient, applicationsTable)
	deploymentsStore := store_dynamodb.NewDeploymentsStore(dynamoDBClient, deploymentsTable)

	dynamoApplications, err := dynamo_store.Get(ctx, "APP_STORE", queryParams)
	if err != nil {
		log.Println(err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       handlerError(handlerName, ErrDynamoDB),
		}, nil
	}

	applications := mappers.DynamoDBApplicationToJsonApplication(dynamoApplications)

	// Fetch and populate deployments for each application
	for i := range applications {
		deployments, err := deploymentsStore.GetHistory(ctx, applications[i].ApplicationId)
		if err != nil {
			log.Printf("error fetching deployments for application %s: %v", applications[i].ApplicationId, err)
			// Continue processing other applications instead of failing completely
			continue
		}
		applications[i].Deployments = mappers.DeploymentItemsToModels(deployments)
	}

	m, err := json.Marshal(applications)
	if err != nil {
		log.Println(err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       handlerError(handlerName, ErrMarshaling),
		}, nil
	}
	response := events.APIGatewayV2HTTPResponse{
		StatusCode: http.StatusOK,
		Body:       string(m),
	}
	return response, nil
}

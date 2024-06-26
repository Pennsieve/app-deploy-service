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
	"github.com/pennsieve/app-deploy-service/service/models"
	"github.com/pennsieve/app-deploy-service/service/store_dynamodb"
)

func GetApplicationHandler(ctx context.Context, request events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	handlerName := "GetApplicationHandler"
	uuid := request.PathParameters["id"]

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

	dynamo_store := store_dynamodb.NewApplicationDatabaseStore(dynamoDBClient, applicationsTable)
	application, err := dynamo_store.GetById(ctx, uuid)
	if err != nil {
		log.Println(err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       handlerError(handlerName, ErrDynamoDB),
		}, nil
	}
	if (store_dynamodb.Application{}) == application {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusNotFound,
			Body:       handlerError(handlerName, ErrNoRecordsFound),
		}, nil
	}

	m, err := json.Marshal(models.Application{
		Uuid:                     application.Uuid,
		ApplicationId:            application.ApplicationId,
		ApplicationContainerName: application.ApplicationContainerName,
		Name:                     application.Name,
		Description:              application.Description,
		Resources: models.ApplicationResources{
			CPU:    application.CPU,
			Memory: application.Memory,
		},
		ApplicationType: application.ApplicationType,
		Account: models.Account{
			Uuid:        application.AccountUuid,
			AccountId:   application.AccountId,
			AccountType: application.AccountType,
		},
		ComputeNode: models.ComputeNode{
			Uuid:  application.ComputeNodeUuid,
			EfsId: application.ComputeNodeEfsId,
		},
		Source: models.Source{
			SourceType: application.SourceType,
			Url:        application.SourceUrl,
		},
		Destination: models.Destination{
			DestinationType: application.DestinationType,
			Url:             application.DestinationUrl,
		},
		Params:           application.Params,
		CommandArguments: application.CommandArguments,
		Env:              application.Env,
		CreatedAt:        application.CreatedAt,
		OrganizationId:   application.OrganizationId,
		UserId:           application.UserId,
		Status:           application.Status,
	})
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

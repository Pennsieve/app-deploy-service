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

	dynamo_store := store_dynamodb.NewNodeDatabaseStore(dynamoDBClient, applicationsTable)
	application, err := dynamo_store.GetById(ctx, uuid)
	if err != nil {
		log.Println(err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       handlerError(handlerName, ErrDynamoDB),
		}, nil
	}
	if (store_dynamodb.App{}) == application {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusNotFound,
			Body:       handlerError(handlerName, ErrNoRecordsFound),
		}, nil
	}

	m, err := json.Marshal(models.Application{
		Uuid:           application.Uuid,
		Name:           application.Name,
		Description:    application.Description,
		AppEcrUrl:      application.AppEcrUrl,
		CreatedAt:      application.CreatedAt,
		OrganizationId: application.OrganizationId,
		UserId:         application.UserId,
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

package handler

import (
	"context"
	"log"
	"net/http"
	"os"

	"github.com/aws/aws-lambda-go/events"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/pennsieve/app-deploy-service/service/store_dynamodb"
	ghsync "github.com/pennsieve/github-client/pkg/github/sync"
)

type contentAppLookup interface {
	GetById(ctx context.Context, uuid string) (*store_dynamodb.AppStoreApplication, error)
}

type contentReader interface {
	Read(ctx context.Context, key string) ([]byte, string, error)
}

func GetAppStoreContentHandler(ctx context.Context, request events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	handlerName := "GetAppStoreContentHandler"

	file := request.QueryStringParameters["file"]
	if file == "" {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusBadRequest,
			Body:       handlerError(handlerName, ErrMissingParams),
		}, nil
	}

	cfg, err := awsconfig.LoadDefaultConfig(ctx)
	if err != nil {
		log.Println(err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       handlerError(handlerName, ErrConfig),
		}, nil
	}

	dynamoDBClient := dynamodb.NewFromConfig(cfg)
	appStore := store_dynamodb.NewAppStoreDatabaseStore(dynamoDBClient, os.Getenv(appstoreApplicationsTableNameKey))

	bucket := os.Getenv("CONTENT_SYNC_BUCKET")
	if bucket == "" {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       handlerError(handlerName, ErrConfig),
		}, nil
	}

	s3Client := s3.NewFromConfig(cfg)
	dest := ghsync.NewS3Destination(s3Client, bucket)

	return getAppStoreContent(ctx, request, appStore, dest)
}

func getAppStoreContent(ctx context.Context, request events.APIGatewayV2HTTPRequest, appStore contentAppLookup, dest contentReader) (events.APIGatewayV2HTTPResponse, error) {
	handlerName := "GetAppStoreContentHandler"

	appId := request.PathParameters["id"]
	file := request.QueryStringParameters["file"]
	tag := request.QueryStringParameters["tag"]

	if file == "" {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusBadRequest,
			Body:       handlerError(handlerName, ErrMissingParams),
		}, nil
	}

	app, err := appStore.GetById(ctx, appId)
	if err != nil {
		log.Println(err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       handlerError(handlerName, ErrDynamoDB),
		}, nil
	}
	if app == nil {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusNotFound,
			Body:       handlerError(handlerName, ErrAppNotFound),
		}, nil
	}

	if tag == "" {
		tag = "main"
	}

	namespace := buildNamespace(app.SourceUrl, tag)
	key := namespace + "/" + file

	data, contentType, err := dest.Read(ctx, key)
	if err != nil {
		log.Printf("error reading %s from S3: %v", key, err)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusNotFound,
			Body:       handlerError(handlerName, ErrNoRecordsFound),
		}, nil
	}

	return events.APIGatewayV2HTTPResponse{
		StatusCode: http.StatusOK,
		Headers:    map[string]string{"Content-Type": contentType},
		Body:       string(data),
	}, nil
}

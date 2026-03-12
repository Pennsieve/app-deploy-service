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
	"github.com/pennsieve/pennsieve-go-core/pkg/authorizer"
)

// GetAppStoreAuthorizeHandler checks whether a user is authorized to pull
// a specific appstore image and returns the ECR image URL.
// This endpoint is called by Account B's Workflow Manager during cross-account
// ECR pull workflows.
//
// Query parameters:
//   - sourceUrl: the git repository URL identifying the application
//   - version: the specific version tag (e.g., "v1.0.7")
//   - userId: the ID of the user requesting access
//
// TODO: Add actual permission model (per-user/per-org access control).
func GetAppStoreAuthorizeHandler(ctx context.Context, request events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	handlerName := "GetAppStoreAuthorizeHandler"

	sourceUrl := request.QueryStringParameters["sourceUrl"]
	version := request.QueryStringParameters["version"]
	userId := request.QueryStringParameters["userId"]

	if sourceUrl == "" || version == "" || userId == "" {
		log.Printf("%s: missing required query parameters: sourceUrl=%q, version=%q, userId=%q", handlerName, sourceUrl, version, userId)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusBadRequest,
			Body:       handlerError(handlerName, ErrMissingParams),
		}, nil
	}

	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		log.Println(err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       handlerError(handlerName, ErrConfig),
		}, nil
	}

	dynamoDBClient := dynamodb.NewFromConfig(cfg)
	applicationsTable := os.Getenv(appstoreApplicationsTableNameKey)
	versionsTable := os.Getenv(appstoreVersionsTableNameKey)
	appStoreStore := store_dynamodb.NewAppStoreDatabaseStore(dynamoDBClient, applicationsTable)
	versionStore := store_dynamodb.NewAppStoreVersionDatabaseStore(dynamoDBClient, versionsTable)

	// Look up the app by sourceUrl
	apps, err := appStoreStore.GetBySourceUrl(ctx, sourceUrl)
	if err != nil {
		log.Printf("%s: error querying appstore: %v", handlerName, err)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       handlerError(handlerName, ErrDynamoDB),
		}, nil
	}

	if len(apps) == 0 {
		resp := models.AuthorizeImageResponse{
			Authorized: false,
			Message:    "application not found in app store",
		}
		m, _ := json.Marshal(resp)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusNotFound,
			Body:       string(m),
		}, nil
	}

	// Look up the specific version
	versions, err := versionStore.GetByApplicationIdAndVersion(ctx, apps[0].Uuid, version)
	if err != nil {
		log.Printf("%s: error querying version: %v", handlerName, err)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       handlerError(handlerName, ErrDynamoDB),
		}, nil
	}

	if len(versions) == 0 {
		resp := models.AuthorizeImageResponse{
			Authorized: false,
			Message:    "version not found for this application",
		}
		m, _ := json.Marshal(resp)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusNotFound,
			Body:       string(m),
		}, nil
	}

	ver := versions[0]
	if ver.DestinationUrl == "" || ver.Status != "deployed" {
		resp := models.AuthorizeImageResponse{
			Authorized: false,
			Message:    "version is not yet deployed",
		}
		m, _ := json.Marshal(resp)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusNotFound,
			Body:       string(m),
		}, nil
	}

	claims := authorizer.ParseClaims(request.RequestContext.Authorizer.Lambda)
	appAccessTable := os.Getenv(appAccessTableNameKey)
	appAccessStore := store_dynamodb.NewAppAccessDatabaseStore(dynamoDBClient, appAccessTable)

	if !CanAccessApp(ctx, claims, &apps[0], appAccessStore) {
		resp := models.AuthorizeImageResponse{
			Authorized: false,
			Message:    "user does not have access to this application",
		}
		m, _ := json.Marshal(resp)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusForbidden,
			Body:       string(m),
		}, nil
	}

	log.Printf("%s: authorizing user %s for image %s (source: %s, version: %s)",
		handlerName, userId, ver.DestinationUrl, sourceUrl, version)

	resp := models.AuthorizeImageResponse{
		Authorized: true,
		ImageUrl:   ver.DestinationUrl,
	}
	m, err := json.Marshal(resp)
	if err != nil {
		log.Println(err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       handlerError(handlerName, ErrMarshaling),
		}, nil
	}

	return events.APIGatewayV2HTTPResponse{
		StatusCode: http.StatusOK,
		Body:       string(m),
	}, nil
}

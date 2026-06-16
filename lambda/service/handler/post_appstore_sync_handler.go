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
	"github.com/pennsieve/app-deploy-service/service/models"
	"github.com/pennsieve/app-deploy-service/service/store_dynamodb"
	"github.com/pennsieve/pennsieve-go-core/pkg/authorizer"
	"github.com/pennsieve/pennsieve-go-core/pkg/models/role"
)

// PostAppStoreSyncHandler syncs source details (e.g. GitHub visibility)
// of an appstore application identified by its source URL. Called by
// the github-service when a repository's details change.
func PostAppStoreSyncHandler(ctx context.Context, request events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	handlerName := "PostAppStoreSyncHandler"

	var req models.AppStoreSync
	if err := json.Unmarshal([]byte(request.Body), &req); err != nil {
		log.Println(err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       handlerError(handlerName, ErrUnmarshaling),
		}, nil
	}

	if req.Source.Url == "" {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusBadRequest,
			Body:       handlerError(handlerName, ErrMissingParams),
		}, nil
	}

	if request.RequestContext.Authorizer != nil && request.RequestContext.Authorizer.Lambda != nil {
		claims := authorizer.ParseClaims(request.RequestContext.Authorizer.Lambda)
		if !authorizer.HasOrgRole(claims, role.Viewer) {
			log.Printf("user not permitted to update visibility with claims: %+v", claims)
			return events.APIGatewayV2HTTPResponse{
				StatusCode: http.StatusUnauthorized,
				Body:       handlerError(handlerName, ErrNotPermitted),
			}, nil
		}
	} else {
		log.Println("direct invocation detected, skipping authorization")
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
	appStoreStore := store_dynamodb.NewAppStoreDatabaseStore(dynamoDBClient, os.Getenv(appstoreApplicationsTableNameKey))

	existingApps, err := appStoreStore.GetBySourceUrl(ctx, req.Source.Url)
	if err != nil {
		log.Println(err)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       handlerError(handlerName, ErrDynamoDB),
		}, nil
	}
	if len(existingApps) == 0 {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusNotFound,
			Body:       handlerError(handlerName, ErrAppNotFound),
		}, nil
	}

	app := existingApps[0]
	newVisibility := req.Source.IsPrivate
	if app.IsPrivate != newVisibility {
		log.Printf("updating visibility for application %s from %v to %v", app.Uuid, app.IsPrivate, newVisibility)
		if err := appStoreStore.UpdateGithubVisibility(ctx, app.Uuid, newVisibility); err != nil {
			log.Println("error updating isPrivate: ", err.Error())
			return events.APIGatewayV2HTTPResponse{
				StatusCode: http.StatusInternalServerError,
				Body:       handlerError(handlerName, ErrDynamoDB),
			}, nil
		}
		app.IsPrivate = newVisibility
	}

	resp := mappers.AppStoreAppToModel(app)
	m, err := json.Marshal(resp)
	if err != nil {
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

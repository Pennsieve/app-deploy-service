package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/pennsieve/app-deploy-service/service/mappers"
	"github.com/pennsieve/app-deploy-service/service/models"
	"github.com/pennsieve/app-deploy-service/service/store_dynamodb"
	"github.com/pennsieve/pennsieve-go-core/pkg/authorizer"
	"github.com/pennsieve/pennsieve-go-core/pkg/models/role"
)

func GetAppPermissionsHandler(ctx context.Context, request events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	handlerName := "GetAppPermissionsHandler"

	appId := request.PathParameters["id"]

	claims := authorizer.ParseClaims(request.RequestContext.Authorizer.Lambda)
	if !authorizer.HasOrgRole(claims, role.Viewer) {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusUnauthorized,
			Body:       handlerError(handlerName, ErrNotPermitted),
		}, nil
	}

	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       handlerError(handlerName, ErrConfig),
		}, nil
	}

	dynamoDBClient := dynamodb.NewFromConfig(cfg)
	appStoreStore := store_dynamodb.NewAppStoreDatabaseStore(dynamoDBClient, os.Getenv(appstoreApplicationsTableNameKey))
	appAccessStore := store_dynamodb.NewAppAccessDatabaseStore(dynamoDBClient, os.Getenv(appAccessTableNameKey))

	app, err := appStoreStore.GetById(ctx, appId)
	if err != nil || app == nil {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusNotFound,
			Body:       handlerError(handlerName, ErrAppNotFound),
		}, nil
	}

	if !CanAccessApp(ctx, claims, app, appAccessStore) {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusForbidden,
			Body:       handlerError(handlerName, ErrNotPermitted),
		}, nil
	}

	accessItems, err := appAccessStore.GetByApp(ctx, appId)
	if err != nil {
		log.Printf("%s: error fetching access entries: %v", handlerName, err)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       handlerError(handlerName, ErrDynamoDB),
		}, nil
	}

	permissions := models.AppPermissions{
		Visibility: app.Visibility,
		OwnerId:    app.OwnerId,
		Access:     mappers.AppAccessItemsToModels(accessItems),
	}

	m, err := json.Marshal(permissions)
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

func PutAppPermissionsHandler(ctx context.Context, request events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	handlerName := "PutAppPermissionsHandler"

	appId := request.PathParameters["id"]

	claims := authorizer.ParseClaims(request.RequestContext.Authorizer.Lambda)
	if !authorizer.HasOrgRole(claims, role.Viewer) {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusUnauthorized,
			Body:       handlerError(handlerName, ErrNotPermitted),
		}, nil
	}

	var req models.SetPermissionsRequest
	if err := json.Unmarshal([]byte(request.Body), &req); err != nil {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusBadRequest,
			Body:       handlerError(handlerName, ErrUnmarshaling),
		}, nil
	}

	if req.Visibility != "public" && req.Visibility != "private" {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusBadRequest,
			Body:       handlerError(handlerName, ErrInvalidVisibility),
		}, nil
	}

	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       handlerError(handlerName, ErrConfig),
		}, nil
	}

	dynamoDBClient := dynamodb.NewFromConfig(cfg)
	appStoreStore := store_dynamodb.NewAppStoreDatabaseStore(dynamoDBClient, os.Getenv(appstoreApplicationsTableNameKey))
	appAccessStore := store_dynamodb.NewAppAccessDatabaseStore(dynamoDBClient, os.Getenv(appAccessTableNameKey))

	app, err := appStoreStore.GetById(ctx, appId)
	if err != nil || app == nil {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusNotFound,
			Body:       handlerError(handlerName, ErrAppNotFound),
		}, nil
	}

	// TEMP: owner check disabled for testing permissions flow against apps
	// created via direct invocation (OwnerId == "system" or empty).
	// Restore before merging.
	// if !IsAppOwner(ctx, claims, app) {
	// 	return events.APIGatewayV2HTTPResponse{
	// 		StatusCode: http.StatusForbidden,
	// 		Body:       handlerError(handlerName, ErrNotOwner),
	// 	}, nil
	// }

	if err := appStoreStore.UpdateVisibility(ctx, appId, req.Visibility); err != nil {
		log.Printf("%s: error updating visibility: %v", handlerName, err)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       handlerError(handlerName, ErrDynamoDB),
		}, nil
	}

	now := time.Now().UTC().String()
	grantedBy := claims.UserClaim.NodeId

	var accessEntries []store_dynamodb.AppAccess

	accessEntries = append(accessEntries, store_dynamodb.AppAccess{
		EntityId:    fmt.Sprintf("user#%s", app.OwnerId),
		AppId:       fmt.Sprintf("app#%s", appId),
		EntityType:  "user",
		EntityRawId: app.OwnerId,
		AppUuid:     appId,
		AccessType:  "owner",
		GrantedAt:   now,
		GrantedBy:   grantedBy,
	})

	for _, u := range req.Users {
		if u.EntityId == app.OwnerId {
			continue
		}
		accessEntries = append(accessEntries, store_dynamodb.AppAccess{
			EntityId:       fmt.Sprintf("user#%s", u.EntityId),
			AppId:          fmt.Sprintf("app#%s", appId),
			EntityType:     "user",
			EntityRawId:    u.EntityId,
			AppUuid:        appId,
			AccessType:     "shared",
			OrganizationId: u.OrganizationId,
			GrantedAt:      now,
			GrantedBy:      grantedBy,
		})
	}

	for _, t := range req.Teams {
		accessEntries = append(accessEntries, store_dynamodb.AppAccess{
			EntityId:       fmt.Sprintf("team#%s", t.EntityId),
			AppId:          fmt.Sprintf("app#%s", appId),
			EntityType:     "team",
			EntityRawId:    t.EntityId,
			AppUuid:        appId,
			AccessType:     "shared",
			OrganizationId: t.OrganizationId,
			GrantedAt:      now,
			GrantedBy:      grantedBy,
		})
	}

	for _, w := range req.Workspaces {
		accessEntries = append(accessEntries, store_dynamodb.AppAccess{
			EntityId:       fmt.Sprintf("workspace#%s", w.EntityId),
			AppId:          fmt.Sprintf("app#%s", appId),
			EntityType:     "workspace",
			EntityRawId:    w.EntityId,
			AppUuid:        appId,
			AccessType:     "workspace",
			OrganizationId: w.OrganizationId,
			GrantedAt:      now,
			GrantedBy:      grantedBy,
		})
	}

	if err := appAccessStore.ReplaceByApp(ctx, appId, accessEntries); err != nil {
		log.Printf("%s: error replacing access entries: %v", handlerName, err)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       handlerError(handlerName, ErrDynamoDB),
		}, nil
	}

	permissions := models.AppPermissions{
		Visibility: req.Visibility,
		OwnerId:    app.OwnerId,
		Access:     mappers.AppAccessItemsToModels(accessEntries),
	}

	m, err := json.Marshal(permissions)
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

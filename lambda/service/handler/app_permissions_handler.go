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
)

func GetAppPermissionsHandler(ctx context.Context, request events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	handlerName := "GetAppPermissionsHandler"

	appId := request.PathParameters["id"]

	// No workspace role is required: the workspace, when there is one, comes
	// from the request and is verified below (workspaceFromRequest); the
	// token's active-organization claim is never used.
	claims := authorizer.ParseClaims(request.RequestContext.Authorizer.Lambda)

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

	// token_auth: the workspace comes from the request, verified here; none
	// means the personal scope. Never claims.OrgClaim (see workspace_access.go).
	ws, errResp := workspaceFromRequest(ctx, request, claims, handlerName)
	if errResp != nil {
		return *errResp, nil
	}
	if !CanAccessAppIn(ctx, claims, ws, app, appAccessStore) {
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

	// Sharing is the owner's, wherever they are (a workspace or My
	// Workspace): IsAppOwner below is the check. The token's active-
	// organization claim is never used.
	claims := authorizer.ParseClaims(request.RequestContext.Authorizer.Lambda)

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

	if !IsAppOwner(ctx, claims, app) {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusForbidden,
			Body:       handlerError(handlerName, ErrNotOwner),
		}, nil
	}

	if err := appStoreStore.UpdateVisibility(ctx, appId, req.Visibility); err != nil {
		log.Printf("%s: error updating visibility: %v", handlerName, err)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       handlerError(handlerName, ErrDynamoDB),
		}, nil
	}

	accessEntries := accessEntriesFor(appId, app.OwnerId, claims.UserClaim.NodeId, time.Now().UTC().String(), req)

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

// accessEntriesFor builds the full access list for an application from a sharing
// request: the owner row first, then every requested user (other than the owner),
// team and workspace. It is what PUT /store/{id}/permissions writes, and what
// POST /store applies when a publish request carries initial sharing.
func accessEntriesFor(appId string, ownerId string, grantedBy string, now string, req models.SetPermissionsRequest) []store_dynamodb.AppAccess {
	accessEntries := []store_dynamodb.AppAccess{{
		EntityId:    fmt.Sprintf("user#%s", ownerId),
		AppId:       fmt.Sprintf("app#%s", appId),
		EntityType:  "user",
		EntityRawId: ownerId,
		AppUuid:     appId,
		AccessType:  "owner",
		GrantedAt:   now,
		GrantedBy:   grantedBy,
	}}

	for _, u := range req.Users {
		if u.EntityId == ownerId {
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

	return accessEntries
}

// initialVisibility is the visibility a new application starts with: what the
// publisher asked for when the request carries valid sharing, otherwise private
// for a private GitHub repository and public for a public one.
func initialVisibility(isPrivate bool, access *models.SetPermissionsRequest) string {
	if access != nil && (access.Visibility == "public" || access.Visibility == "private") {
		return access.Visibility
	}
	if isPrivate {
		return "private"
	}
	return "public"
}

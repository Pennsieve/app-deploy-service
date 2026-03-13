package handler

import (
	"context"
	"fmt"

	"github.com/pennsieve/pennsieve-go-core/pkg/authorizer"
	"github.com/pennsieve/app-deploy-service/service/store_dynamodb"
)

func CanAccessApp(ctx context.Context, claims *authorizer.Claims, app *store_dynamodb.AppStoreApplication, accessStore *store_dynamodb.AppAccessDatabaseStore) bool {
	if app.Visibility == "public" {
		return true
	}

	userId := claims.UserClaim.NodeId
	if app.OwnerId == userId {
		return true
	}

	userEntityId := fmt.Sprintf("user#%s", userId)
	appId := fmt.Sprintf("app#%s", app.Uuid)
	access, err := accessStore.GetAccess(ctx, userEntityId, appId)
	if err == nil && access != nil {
		return true
	}

	workspaceEntityId := fmt.Sprintf("workspace#%s", claims.OrgClaim.NodeId)
	access, err = accessStore.GetAccess(ctx, workspaceEntityId, appId)
	if err == nil && access != nil {
		return true
	}

	for _, teamClaim := range claims.TeamClaims {
		teamEntityId := fmt.Sprintf("team#%s", teamClaim.NodeId)
		access, err = accessStore.GetAccess(ctx, teamEntityId, appId)
		if err == nil && access != nil {
			return true
		}
	}

	return false
}

func IsAppOwner(ctx context.Context, claims *authorizer.Claims, app *store_dynamodb.AppStoreApplication) bool {
	return app.OwnerId == claims.UserClaim.NodeId
}

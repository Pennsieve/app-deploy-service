package handler

import (
	"context"

	"github.com/pennsieve/app-deploy-service/service/access"
	"github.com/pennsieve/app-deploy-service/service/store_dynamodb"
	"github.com/pennsieve/pennsieve-go-core/pkg/authorizer"
)

// CanAccessApp treats a failed grant lookup as no grant, as it always has.
// Only for routes under token_workspace_auth, whose organization and team
// claims the gateway verified for the request's workspace. On token_auth
// routes use CanAccessAppIn with workspaceFromRequest.
func CanAccessApp(ctx context.Context, claims *authorizer.Claims, app *store_dynamodb.AppStoreApplication, accessStore *store_dynamodb.AppAccessDatabaseStore) bool {
	if app.Visibility == "public" {
		return true
	}
	if claims.UserClaim == nil {
		return false
	}
	var workspaceIds []string
	if claims.OrgClaim != nil {
		workspaceIds = []string{claims.OrgClaim.NodeId}
	}
	teamIds := make([]string, 0, len(claims.TeamClaims))
	for _, teamClaim := range claims.TeamClaims {
		teamIds = append(teamIds, teamClaim.NodeId)
	}
	ok, _ := access.HasAppAccess(ctx, app, claims.UserClaim.NodeId, workspaceIds, teamIds, accessStore)
	return ok
}

// CanAccessAppPersonally is CanAccessApp outside any workspace: public apps,
// apps the user owns, and apps shared with the user directly. Workspace and
// team grants don't count, whatever the claims carry, so a user token with a
// default organization doesn't pull that workspace's apps in.
func CanAccessAppPersonally(ctx context.Context, claims *authorizer.Claims, app *store_dynamodb.AppStoreApplication, accessStore *store_dynamodb.AppAccessDatabaseStore) bool {
	if app.Visibility == "public" {
		return true
	}
	if claims.UserClaim == nil {
		return false
	}
	ok, _ := access.HasAppAccess(ctx, app, claims.UserClaim.NodeId, nil, nil, accessStore)
	return ok
}

func IsAppOwner(ctx context.Context, claims *authorizer.Claims, app *store_dynamodb.AppStoreApplication) bool {
	return app.OwnerId == claims.UserClaim.NodeId
}

// Package access decides whether a user may see an appstore application. It is
// shared by the service Lambda's handlers and the check-app-access Lambda.
package access

import (
	"context"
	"errors"
	"fmt"

	"github.com/pennsieve/app-deploy-service/service/store_dynamodb"
)

// GrantLookup finds an access grant for an entity (user#…, workspace#…, team#…) on an app (app#…).
type GrantLookup interface {
	GetAccess(ctx context.Context, entityId string, appId string) (*store_dynamodb.AppAccess, error)
}

// AppLookup finds an appstore application by uuid; it returns nil, nil when there is none.
type AppLookup interface {
	GetById(ctx context.Context, uuid string) (*store_dynamodb.AppStoreApplication, error)
}

// HasAppAccess reports whether the user may access app: the app is public, the user owns it, or there is a
// grant for the user, any of the workspaces, or any of the teams.
//
// A failed grant lookup does not stop the remaining lookups, so a grant found elsewhere still returns true.
// The error is non-nil only when no grant was found and at least one lookup failed.
func HasAppAccess(ctx context.Context, app *store_dynamodb.AppStoreApplication, userNodeId string, workspaceNodeIds []string, teamNodeIds []string, grants GrantLookup) (bool, error) {
	if app.Visibility == "public" {
		return true, nil
	}
	if app.OwnerId == userNodeId {
		return true, nil
	}

	entityIds := make([]string, 0, 1+len(workspaceNodeIds)+len(teamNodeIds))
	entityIds = append(entityIds, fmt.Sprintf("user#%s", userNodeId))
	for _, ws := range workspaceNodeIds {
		entityIds = append(entityIds, fmt.Sprintf("workspace#%s", ws))
	}
	for _, team := range teamNodeIds {
		entityIds = append(entityIds, fmt.Sprintf("team#%s", team))
	}

	appId := fmt.Sprintf("app#%s", app.Uuid)
	var errs []error
	for _, entityId := range entityIds {
		grant, err := grants.GetAccess(ctx, entityId, appId)
		if err != nil {
			errs = append(errs, fmt.Errorf("looking up access for %s on %s: %w", entityId, appId, err))
			continue
		}
		if grant != nil {
			return true, nil
		}
	}
	return false, errors.Join(errs...)
}

// CheckRequest is the check-app-access Lambda's input.
type CheckRequest struct {
	AppUuid          string   `json:"appUuid"`
	UserNodeId       string   `json:"userNodeId"`
	WorkspaceNodeIds []string `json:"workspaceNodeIds"`
	TeamNodeIds      []string `json:"teamNodeIds"`
}

// CheckResponse is the check-app-access Lambda's output.
type CheckResponse struct {
	HasAccess bool `json:"hasAccess"`
}

// Checker answers CheckRequests against the appstore applications and app access tables.
type Checker struct {
	Apps   AppLookup
	Grants GrantLookup
}

func NewChecker(apps AppLookup, grants GrantLookup) *Checker {
	return &Checker{Apps: apps, Grants: grants}
}

// Check returns hasAccess=false for an unknown app or a request without an app or user, and an error only
// when a store lookup fails.
func (c *Checker) Check(ctx context.Context, req CheckRequest) (CheckResponse, error) {
	if req.AppUuid == "" || req.UserNodeId == "" {
		return CheckResponse{HasAccess: false}, nil
	}
	app, err := c.Apps.GetById(ctx, req.AppUuid)
	if err != nil {
		return CheckResponse{}, fmt.Errorf("looking up app %s: %w", req.AppUuid, err)
	}
	if app == nil {
		return CheckResponse{HasAccess: false}, nil
	}
	ok, err := HasAppAccess(ctx, app, req.UserNodeId, req.WorkspaceNodeIds, req.TeamNodeIds, c.Grants)
	if err != nil {
		return CheckResponse{}, err
	}
	return CheckResponse{HasAccess: ok}, nil
}

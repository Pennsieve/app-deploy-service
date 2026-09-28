// Package access decides whether a user may see an application. HasAppAccess is shared by the service
// Lambda's handlers and the check-app-access Lambda; Checker is the check-app-access Lambda's logic.
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

// ApplicationLookup finds a compute-node application by uuid; it returns a zero Application when there is none.
type ApplicationLookup interface {
	GetById(ctx context.Context, uuid string) (store_dynamodb.Application, error)
}

// VersionLookup finds an appstore version by uuid; it returns nil, nil when there is none.
type VersionLookup interface {
	GetById(ctx context.Context, uuid string) (*store_dynamodb.AppStoreVersion, error)
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

// Checker answers CheckRequests. The app uuid on a status channel is an appstore application, a compute-node
// application or an appstore version, so it is looked up in that order.
type Checker struct {
	Apps         AppLookup
	Applications ApplicationLookup
	Versions     VersionLookup
	Grants       GrantLookup
}

func NewChecker(apps AppLookup, applications ApplicationLookup, versions VersionLookup, grants GrantLookup) *Checker {
	return &Checker{Apps: apps, Applications: applications, Versions: versions, Grants: grants}
}

// Check returns hasAccess=false for an id found in none of the tables or a request without an app or user,
// and an error only when a store lookup fails.
func (c *Checker) Check(ctx context.Context, req CheckRequest) (CheckResponse, error) {
	if req.AppUuid == "" || req.UserNodeId == "" {
		return CheckResponse{HasAccess: false}, nil
	}

	app, err := c.Apps.GetById(ctx, req.AppUuid)
	if err != nil {
		return CheckResponse{}, fmt.Errorf("looking up appstore application %s: %w", req.AppUuid, err)
	}
	if app != nil {
		return c.checkAppStoreApp(ctx, app, req)
	}

	application, err := c.Applications.GetById(ctx, req.AppUuid)
	if err != nil {
		return CheckResponse{}, fmt.Errorf("looking up application %s: %w", req.AppUuid, err)
	}
	if application.Uuid != "" {
		return CheckResponse{HasAccess: HasApplicationAccess(application, req.UserNodeId, req.WorkspaceNodeIds)}, nil
	}

	version, err := c.Versions.GetById(ctx, req.AppUuid)
	if err != nil {
		return CheckResponse{}, fmt.Errorf("looking up appstore version %s: %w", req.AppUuid, err)
	}
	if version == nil || version.ApplicationId == "" {
		return CheckResponse{HasAccess: false}, nil
	}
	app, err = c.Apps.GetById(ctx, version.ApplicationId)
	if err != nil {
		return CheckResponse{}, fmt.Errorf("looking up appstore application %s of version %s: %w", version.ApplicationId, req.AppUuid, err)
	}
	if app == nil {
		return CheckResponse{HasAccess: false}, nil
	}
	return c.checkAppStoreApp(ctx, app, req)
}

func (c *Checker) checkAppStoreApp(ctx context.Context, app *store_dynamodb.AppStoreApplication, req CheckRequest) (CheckResponse, error) {
	ok, err := HasAppAccess(ctx, app, req.UserNodeId, req.WorkspaceNodeIds, req.TeamNodeIds, c.Grants)
	if err != nil {
		return CheckResponse{}, err
	}
	return CheckResponse{HasAccess: ok}, nil
}

// HasApplicationAccess reports whether the user may see a compute-node application: it belongs to one of the
// workspaces, or the user created it.
func HasApplicationAccess(application store_dynamodb.Application, userNodeId string, workspaceNodeIds []string) bool {
	if userNodeId != "" && application.UserId == userNodeId {
		return true
	}
	if application.OrganizationId == "" {
		return false
	}
	for _, ws := range workspaceNodeIds {
		if ws == application.OrganizationId {
			return true
		}
	}
	return false
}

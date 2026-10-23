package handler

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/aws/aws-lambda-go/events"
	"github.com/pennsieve/app-deploy-service/service/store_dynamodb"
	"github.com/pennsieve/pennsieve-go-core/pkg/models/teamUser"
	"github.com/stretchr/testify/assert"
)

// stubDirectAuthorize replaces the direct authorizer for one test.
func stubDirectAuthorize(t *testing.T, fn func(directAuthorizeRequest) (directAuthorizeResponse, error)) *[]directAuthorizeRequest {
	t.Helper()
	var calls []directAuthorizeRequest
	orig := directAuthorize
	directAuthorize = func(_ context.Context, req directAuthorizeRequest) (directAuthorizeResponse, error) {
		calls = append(calls, req)
		return fn(req)
	}
	t.Cleanup(func() { directAuthorize = orig })
	return &calls
}

// memberResponse is the direct authorizer's answer for a member of org with
// the given role (0 = NoPermission) and teams there.
func memberResponse(org string, role float64, teams ...string) directAuthorizeResponse {
	var teamClaims []interface{}
	for _, t := range teams {
		teamClaims = append(teamClaims, map[string]interface{}{"NodeId": t, "Name": t, "TeamType": "team", "IntId": float64(1), "Permission": float64(16)})
	}
	return directAuthorizeResponse{
		IsAuthorized: true,
		Claims: map[string]interface{}{
			"user_claim":  map[string]interface{}{"Id": float64(1), "NodeId": "N:user:me", "IsSuperAdmin": false},
			"org_claim":   map[string]interface{}{"Role": role, "IntId": float64(7), "NodeId": org},
			"team_claims": teamClaims,
		},
	}
}

func requestFor(org string) events.APIGatewayV2HTTPRequest {
	req := events.APIGatewayV2HTTPRequest{}
	if org != "" {
		req.QueryStringParameters = map[string]string{"organization_id": org}
	}
	return req
}

func TestWorkspaceFromRequest_NoneNamed(t *testing.T) {
	calls := stubDirectAuthorize(t, func(directAuthorizeRequest) (directAuthorizeResponse, error) {
		t.Fatal("direct authorizer called without organization_id")
		return directAuthorizeResponse{}, nil
	})
	ws, errResp := workspaceFromRequest(context.Background(), requestFor(""), newTestClaims("N:user:me", "N:org:active", nil), "test")
	assert.Nil(t, ws)
	assert.Nil(t, errResp)
	assert.Empty(t, *calls)

	_, errResp = requireWorkspaceFromRequest(context.Background(), requestFor(""), newTestClaims("N:user:me", "N:org:active", nil), "test")
	if assert.NotNil(t, errResp) {
		assert.Equal(t, http.StatusBadRequest, errResp.StatusCode, "the claims' (active) organization must not stand in")
	}
}

func TestWorkspaceFromRequest_VerifiedMember(t *testing.T) {
	calls := stubDirectAuthorize(t, func(req directAuthorizeRequest) (directAuthorizeResponse, error) {
		return memberResponse(req.OrganizationNodeID, 8, "N:team:t1"), nil
	})
	// The claims carry a different (active) organization: the request's wins.
	ws, errResp := workspaceFromRequest(context.Background(), requestFor("N:org:viewed"), newTestClaims("N:user:me", "N:org:active", nil), "test")
	assert.Nil(t, errResp)
	if assert.NotNil(t, ws) {
		assert.Equal(t, "N:org:viewed", ws.OrgNodeId)
		assert.Equal(t, []string{"N:team:t1"}, ws.TeamNodeIds)
		assert.Equal(t, "N:org:viewed", ws.Claims.OrgClaim.NodeId)
	}
	assert.Equal(t, []directAuthorizeRequest{{UserNodeID: "N:user:me", OrganizationNodeID: "N:org:viewed"}}, *calls)
}

func TestWorkspaceFromRequest_Refused(t *testing.T) {
	cases := map[string]func(directAuthorizeRequest) (directAuthorizeResponse, error){
		"not a member": func(directAuthorizeRequest) (directAuthorizeResponse, error) {
			return directAuthorizeResponse{IsAuthorized: false, Error: "unable to get organization claim"}, nil
		},
		"no permission": func(req directAuthorizeRequest) (directAuthorizeResponse, error) {
			return memberResponse(req.OrganizationNodeID, 0), nil
		},
		"claim for another organization": func(directAuthorizeRequest) (directAuthorizeResponse, error) {
			return memberResponse("N:org:other", 8), nil
		},
	}
	for name, fn := range cases {
		t.Run(name, func(t *testing.T) {
			stubDirectAuthorize(t, fn)
			ws, errResp := workspaceFromRequest(context.Background(), requestFor("N:org:viewed"), newTestClaims("N:user:me", "N:org:viewed", nil), "test")
			assert.Nil(t, ws)
			if assert.NotNil(t, errResp) {
				assert.Equal(t, http.StatusForbidden, errResp.StatusCode)
			}
		})
	}
}

func TestWorkspaceFromRequest_CheckFails(t *testing.T) {
	stubDirectAuthorize(t, func(directAuthorizeRequest) (directAuthorizeResponse, error) {
		return directAuthorizeResponse{}, errors.New("throttled")
	})
	_, errResp := workspaceFromRequest(context.Background(), requestFor("N:org:viewed"), newTestClaims("N:user:me", "", nil), "test")
	if assert.NotNil(t, errResp) {
		assert.Equal(t, http.StatusInternalServerError, errResp.StatusCode)
	}
}

func TestCanAccessAppIn(t *testing.T) {
	ctx := context.Background()
	private := &store_dynamodb.AppStoreApplication{Uuid: "app-uuid", Visibility: "private", OwnerId: "N:user:owner"}
	me := newTestClaims("N:user:me", "N:org:active", []teamUser.Claim{{NodeId: "N:team:active-team"}})

	viewed := &requestWorkspace{OrgNodeId: "N:org:viewed", TeamNodeIds: []string{"N:team:viewed-team"}}
	assert.True(t, CanAccessAppIn(ctx, me, viewed, private, grantFor("workspace#N:org:viewed", "workspace")), "shared with the verified workspace")
	assert.True(t, CanAccessAppIn(ctx, me, viewed, private, grantFor("team#N:team:viewed-team", "team")), "shared with a team in it")

	// The claims' organization and teams (the LEGACY active organization) don't count.
	assert.False(t, CanAccessAppIn(ctx, me, viewed, private, grantFor("workspace#N:org:active", "workspace")))
	assert.False(t, CanAccessAppIn(ctx, me, viewed, private, grantFor("team#N:team:active-team", "team")))
	assert.False(t, CanAccessAppIn(ctx, me, nil, private, grantFor("workspace#N:org:active", "workspace")), "no workspace: personal only")
	assert.True(t, CanAccessAppIn(ctx, me, nil, private, grantFor("user#N:user:me", "user")), "no workspace: shared with me")
}

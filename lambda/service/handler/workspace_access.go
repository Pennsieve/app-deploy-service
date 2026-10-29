package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	lambdatypes "github.com/aws/aws-sdk-go-v2/service/lambda/types"
	"github.com/pennsieve/app-deploy-service/service/access"
	"github.com/pennsieve/app-deploy-service/service/store_dynamodb"
	"github.com/pennsieve/pennsieve-go-core/pkg/authorizer"
	"github.com/pennsieve/pennsieve-go-core/pkg/models/pgdb"
)

// Routes under token_auth get no workspace from the gateway: the user-token
// authorizer's organization claim (in LEGACY mode, the user's active or
// preferred organization) isn't the workspace of the request, and isn't
// there at all otherwise. So these routes take the workspace from the
// request's organization_id and verify it here, through the platform's direct
// authorizer, as workflow-service does. Never use claims.OrgClaim on them.

// requestWorkspace is a workspace a request named and the caller belongs to.
type requestWorkspace struct {
	OrgNodeId   string
	TeamNodeIds []string // the caller's teams in that workspace
	// Claims are the verified claims in that workspace (user, organization
	// with the caller's role, teams), for role checks such as HasOrgRole.
	Claims *authorizer.Claims
}

type directAuthorizeRequest struct {
	UserNodeID         string `json:"user_node_id"`
	OrganizationNodeID string `json:"organization_node_id,omitempty"`
}

type directAuthorizeResponse struct {
	IsAuthorized bool                   `json:"is_authorized"`
	Claims       map[string]interface{} `json:"claims,omitempty"`
	Error        string                 `json:"error,omitempty"`
}

// directAuthorize invokes the direct authorizer Lambda. A variable so tests
// can stub it.
var directAuthorize = func(ctx context.Context, req directAuthorizeRequest) (directAuthorizeResponse, error) {
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return directAuthorizeResponse{}, fmt.Errorf("load AWS config: %w", err)
	}
	payload, err := json.Marshal(req)
	if err != nil {
		return directAuthorizeResponse{}, err
	}
	out, err := lambda.NewFromConfig(cfg).Invoke(ctx, &lambda.InvokeInput{
		FunctionName:   aws.String(os.Getenv("DIRECT_AUTHORIZER_LAMBDA_NAME")),
		InvocationType: lambdatypes.InvocationTypeRequestResponse,
		Payload:        payload,
	})
	if err != nil {
		return directAuthorizeResponse{}, fmt.Errorf("invoke direct authorizer: %w", err)
	}
	var resp directAuthorizeResponse
	if err := json.Unmarshal(out.Payload, &resp); err != nil {
		return directAuthorizeResponse{}, fmt.Errorf("decode direct authorizer response: %w", err)
	}
	return resp, nil
}

// workspaceFromRequest returns the workspace the request names in
// organization_id, once the caller's membership is verified; nil when it
// names none. On failure it returns the response to send: 403 when the
// caller isn't a member (or has no permission there), 500 when the check
// itself fails.
func workspaceFromRequest(ctx context.Context, request events.APIGatewayV2HTTPRequest, claims *authorizer.Claims, handlerName string) (*requestWorkspace, *events.APIGatewayV2HTTPResponse) {
	orgNodeId := request.QueryStringParameters["organization_id"]
	if orgNodeId == "" {
		return nil, nil
	}
	if claims == nil || claims.UserClaim == nil {
		return nil, &events.APIGatewayV2HTTPResponse{StatusCode: http.StatusUnauthorized, Body: handlerError(handlerName, ErrNotPermitted)}
	}

	resp, err := directAuthorize(ctx, directAuthorizeRequest{UserNodeID: claims.UserClaim.NodeId, OrganizationNodeID: orgNodeId})
	if err != nil {
		log.Printf("%s: verifying workspace %s for %s: %v", handlerName, orgNodeId, claims.UserClaim.NodeId, err)
		return nil, &events.APIGatewayV2HTTPResponse{StatusCode: http.StatusInternalServerError, Body: handlerError(handlerName, ErrWorkspaceCheck)}
	}
	verified := authorizer.ParseClaims(resp.Claims)
	if !resp.IsAuthorized || verified.OrgClaim == nil || verified.OrgClaim.NodeId != orgNodeId || verified.OrgClaim.Role == pgdb.NoPermission {
		log.Printf("%s: %s has no access to workspace %s: %s", handlerName, claims.UserClaim.NodeId, orgNodeId, resp.Error)
		return nil, &events.APIGatewayV2HTTPResponse{StatusCode: http.StatusForbidden, Body: handlerError(handlerName, ErrNotPermitted)}
	}

	ws := &requestWorkspace{OrgNodeId: orgNodeId, Claims: verified}
	for _, t := range verified.TeamClaims {
		ws.TeamNodeIds = append(ws.TeamNodeIds, t.NodeId)
	}
	return ws, nil
}

// requireWorkspaceFromRequest is workspaceFromRequest for routes that act in
// a workspace: naming none is a 400.
func requireWorkspaceFromRequest(ctx context.Context, request events.APIGatewayV2HTTPRequest, claims *authorizer.Claims, handlerName string) (*requestWorkspace, *events.APIGatewayV2HTTPResponse) {
	ws, errResp := workspaceFromRequest(ctx, request, claims, handlerName)
	if errResp != nil {
		return nil, errResp
	}
	if ws == nil {
		return nil, &events.APIGatewayV2HTTPResponse{StatusCode: http.StatusBadRequest, Body: handlerError(handlerName, fmt.Errorf("organization_id is required"))}
	}
	return ws, nil
}

// CanAccessAppIn is the app access check for a token_auth route: in the
// verified workspace ws (with the caller's teams there), or personally (public,
// owned, shared with the caller directly) when ws is nil.
func CanAccessAppIn(ctx context.Context, claims *authorizer.Claims, ws *requestWorkspace, app *store_dynamodb.AppStoreApplication, accessStore *store_dynamodb.AppAccessDatabaseStore) bool {
	if ws == nil {
		return CanAccessAppPersonally(ctx, claims, app, accessStore)
	}
	if app.Visibility == "public" {
		return true
	}
	if claims.UserClaim == nil {
		return false
	}
	ok, _ := access.HasAppAccess(ctx, app, claims.UserClaim.NodeId, []string{ws.OrgNodeId}, ws.TeamNodeIds, accessStore)
	return ok
}

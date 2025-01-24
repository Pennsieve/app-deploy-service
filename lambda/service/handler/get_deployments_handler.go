package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/pennsieve/app-deploy-service/service/mappers"
	"github.com/pennsieve/app-deploy-service/service/models"
	"github.com/pennsieve/app-deploy-service/service/store_dynamodb"
	"github.com/pennsieve/pennsieve-go-core/pkg/authorizer"
	"github.com/pennsieve/pennsieve-go-core/pkg/models/role"
	"net/http"
	"os"
)

func GetDeploymentsHandler(ctx context.Context, request events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	handlerName := "GetDeploymentsHandler"

	claims := authorizer.ParseClaims(request.RequestContext.Authorizer.Lambda)
	if !authorizer.HasOrgRole(claims, role.Viewer) {
		responseErr := logError(handlerName, "user not permitted to view deployments for workspace", nil)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusUnauthorized,
			Body:       handlerError(handlerName, responseErr),
		}, nil
	}

	expectedOrganizationId := claims.OrgClaim.NodeId

	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		responseErr := logError(handlerName, "error getting AWS config", err)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       handlerError(handlerName, responseErr),
		}, nil
	}
	deploymentsTable := os.Getenv(deploymentsTableNameKey)
	if len(deploymentsTable) == 0 {
		responseErr := logError(handlerName, "missing deployments table env var value", nil)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       handlerError(handlerName, responseErr),
		}, nil
	}
	deploymentsStore := store_dynamodb.NewDeploymentsStore(dynamodb.NewFromConfig(cfg), deploymentsTable)

	applicationId := request.PathParameters["id"]
	if len(applicationId) == 0 {
		responseErr := logError(handlerName, "missing path parameter 'id'", nil)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusBadRequest,
			Body:       handlerError(handlerName, responseErr),
		}, nil
	}

	deploymentItems, err := deploymentsStore.GetHistory(ctx, applicationId)
	if err != nil {
		responseErr := logError(handlerName, fmt.Sprintf("error getting application %s deployments", applicationId), err)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       handlerError(handlerName, responseErr),
		}, nil
	}

	if !IsAuthorized(expectedOrganizationId, deploymentItems...) {
		responseErr := logError(handlerName, "user not permitted to view deployment", nil)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusUnauthorized,
			Body:       handlerError(handlerName, responseErr),
		}, nil
	}

	var deployments models.Deployments

	deployments.Deployments = mappers.DeploymentItemsToModels(deploymentItems)

	//TODO sort

	response, err := json.Marshal(deployments)
	if err != nil {
		responseErr := logError(handlerName, "error marshalling response", err)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       handlerError(handlerName, responseErr),
		}, nil
	}

	return events.APIGatewayV2HTTPResponse{
		StatusCode: http.StatusOK,
		Body:       string(response),
	}, nil
}

// IsAuthorized just checks that all the deployments in deploymentItems have the currentWorkspaceId (the one from the claims).
// Maybe later we'll do something with the user?
func IsAuthorized(currentWorkspaceId string, deploymentItems ...store_dynamodb.Deployment) bool {
	for _, deploymentItem := range deploymentItems {
		if deploymentItem.WorkspaceNodeId != currentWorkspaceId {
			return false
		}
	}
	return true
}

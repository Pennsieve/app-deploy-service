package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/pennsieve/app-deploy-service/service/mappers"
	"github.com/pennsieve/app-deploy-service/service/store_dynamodb"
	"github.com/pennsieve/pennsieve-go-core/pkg/authorizer"
	"github.com/pennsieve/pennsieve-go-core/pkg/models/role"
	"log"
	"net/http"
	"os"
)

func GetDeploymentStatusHandler(ctx context.Context, request events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	handlerName := "GetDeploymentHandler"

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
	deploymentId := request.PathParameters["deploymentId"]
	if len(deploymentId) == 0 {
		responseErr := logError(handlerName, "missing path parameter 'deploymentId'", nil)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusBadRequest,
			Body:       handlerError(handlerName, responseErr),
		}, nil
	}

	deploymentItem, err := deploymentsStore.Get(ctx, applicationId, deploymentId)
	if err != nil {
		responseErr := logError(handlerName, fmt.Sprintf("error getting deployment %s", deploymentId), err)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       handlerError(handlerName, responseErr),
		}, nil
	}

	if deploymentItem == nil {
		responseErr := logError(handlerName, fmt.Sprintf("deployment %s not found", deploymentId), nil)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusNotFound,
			Body:       handlerError(handlerName, responseErr),
		}, nil
	}

	if deploymentItem.WorkspaceNodeId != expectedOrganizationId {
		responseErr := logError(handlerName, "user not permitted to view deployment", nil)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusUnauthorized,
			Body:       handlerError(handlerName, responseErr),
		}, nil
	}

	deployment := mappers.DeploymentItemToModel(*deploymentItem)

	response, err := json.Marshal(deployment)
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

func logError(handlerName string, msg string, err error) error {
	var fullError error
	if err == nil {
		fullError = errors.New(msg)
	} else {
		fullError = fmt.Errorf("%s: %w", msg, err)
	}
	log.Printf("%s: %s", handlerName, fullError.Error())
	return fullError
}

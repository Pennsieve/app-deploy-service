package handler

import (
	"context"
	"github.com/aws/aws-lambda-go/events"
)

func GetDeploymentStatusHandler(ctx context.Context, request events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	handlerName := "GetDeploymentHandler"
	applicationId := request.PathParameters["id"]
	deploymentId := request.PathParameters["deploymentId"]
}

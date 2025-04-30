package handler

import (
	"context"
	"log/slog"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambdacontext"
	"github.com/pennsieve/app-deploy-service/service/logging"
)

var logger = logging.Default

func init() {
	logger.Info("init()")
}

func AppDeployServiceHandler(ctx context.Context, request events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	if lc, ok := lambdacontext.FromContext(ctx); ok {
		logger = logger.With(slog.String("requestID", lc.AwsRequestID))
	}

	logger.Info("request parameters",
		"routeKey", request.RouteKey,
		"pathParameters", request.PathParameters,
		"rawPath", request.RawPath,
		"requestContext.routeKey", request.RequestContext.RouteKey,
		"requestContext.http.path", request.RequestContext.HTTP.Path)

	router := NewLambdaRouter()
	// register routes based on their supported methods
	router.POST("/applications", PostApplicationsHandler)
	router.GET("/applications", GetApplicationsHandler)
	router.GET("/applications/{id}", GetApplicationHandler)
	router.GET("/applications/{id}/deployments", GetDeploymentsHandler)
	router.GET("/applications/{id}/deployments/{deploymentId}", GetDeploymentHandler)
	router.DELETE("/applications/{id}", DeleteApplicationHandler)
	router.PUT("/applications/{id}", PutApplicationsHandler)
	router.POST("/applications/deploy", PostApplicationDeployHandler)
	router.POST("/applications/store", PostAppStoreHandler)

	return router.Start(ctx, request)
}

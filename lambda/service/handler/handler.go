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
	router.POST("/", PostApplicationsHandler)
	router.GET("/", GetApplicationsHandler)
	router.GET("/{id}", GetApplicationHandler)
	router.GET("/{id}/deployments", GetDeploymentsHandler)
	router.GET("/{id}/deployments/{deploymentId}", GetDeploymentHandler)
	router.DELETE("/{id}", DeleteApplicationHandler)
	router.PUT("/{id}", PutApplicationsHandler)
	router.POST("/deploy", PostApplicationDeployHandler)

	// AppStore routes
	router.POST("/store", PostAppStoreHandler)
	router.GET("/store", GetAppstoreApplicationsHandler)
	router.GET("/store/authorize", GetAppStoreAuthorizeHandler)

	// AppStore permission routes
	router.GET("/store/{id}/permissions", GetAppPermissionsHandler)
	router.PUT("/store/{id}/permissions", PutAppPermissionsHandler)

	return router.Start(ctx, request)
}

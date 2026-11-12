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
	// Rebuilt from Default on every invocation: deriving from the previous
	// logger would stack one requestID attribute per warm invocation.
	logger = logging.Default
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
	// Deployment history (App Store builds included). The routes that
	// installed apps on compute nodes before the App Store are retired.
	router.GET("/{id}/deployments", GetDeploymentsHandler)
	router.GET("/{id}/deployments/{deploymentId}", GetDeploymentHandler)

	// AppStore routes
	router.POST("/store", PostAppStoreHandler)
	router.GET("/store", GetAppstoreApplicationsHandler)
	router.GET("/store/me", GetMyAppstoreApplicationsHandler)
	router.GET("/store/registry", GetAppStoreRegistryHandler)

	// AppStore sync route
	router.POST("/store/sync", PostAppStoreSyncHandler)

	// AppStore application detail route
	router.GET("/store/{id}", GetAppstoreApplicationHandler)
	router.PATCH("/store/{id}", PatchAppstoreApplicationHandler)

	// AppStore asset routes
	router.GET("/store/{id}/asset", GetAppStoreAssetHandler)

	// AppStore permission routes
	router.GET("/store/{id}/permissions", GetAppPermissionsHandler)
	router.PUT("/store/{id}/permissions", PutAppPermissionsHandler)

	// A version's sample run on the owner's compute node (sample_run_node.go)
	router.GET("/store/{id}/versions/{versionId}/sample-run/plan", GetSampleRunPlanHandler)
	router.POST("/store/{id}/versions/{versionId}/sample-run", PostSampleRunHandler)
	router.POST("/store/{id}/versions/{versionId}/sample-run/results", PostSampleRunResultsHandler)

	return router.Start(ctx, request)
}

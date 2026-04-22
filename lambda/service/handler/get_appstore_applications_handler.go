package handler

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/pennsieve/app-deploy-service/service/mappers"
	"github.com/pennsieve/app-deploy-service/service/store_dynamodb"
	"github.com/pennsieve/pennsieve-go-core/pkg/authorizer"
)

func GetAppstoreApplicationsHandler(ctx context.Context, request events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	handlerName := "GetAppstoreApplicationsHandler"

	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		log.Println(err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       handlerError(handlerName, ErrConfig),
		}, nil
	}
	dynamoDBClient := dynamodb.NewFromConfig(cfg)
	applicationsTable := os.Getenv(appstoreApplicationsTableNameKey)
	versionsTable := os.Getenv(appstoreVersionsTableNameKey)
	deploymentsTable := os.Getenv(deploymentsTableNameKey)

	appStoreStore := store_dynamodb.NewAppStoreDatabaseStore(dynamoDBClient, applicationsTable)
	versionStore := store_dynamodb.NewAppStoreVersionDatabaseStore(dynamoDBClient, versionsTable)
	deploymentsStore := store_dynamodb.NewDeploymentsStore(dynamoDBClient, deploymentsTable)
	appAccessTable := os.Getenv(appAccessTableNameKey)
	appAccessStore := store_dynamodb.NewAppAccessDatabaseStore(dynamoDBClient, appAccessTable)

	claims := authorizer.ParseClaims(request.RequestContext.Authorizer.Lambda)
	log.Printf("%s: caller org=%s user=%s", handlerName, claims.OrgClaim.NodeId, claims.UserClaim.NodeId)

	// Get all apps (or filter by sourceUrl if provided)
	queryParams := request.QueryStringParameters
	var dynamoApps []store_dynamodb.AppStoreApplication
	if sourceUrl, found := queryParams["sourceUrl"]; found {
		dynamoApps, err = appStoreStore.GetBySourceUrl(ctx, sourceUrl)
	} else {
		dynamoApps, err = appStoreStore.GetAll(ctx)
	}
	if err != nil {
		log.Println(err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       handlerError(handlerName, ErrDynamoDB),
		}, nil
	}

	var filteredApps []store_dynamodb.AppStoreApplication
	for _, app := range dynamoApps {
		if CanAccessApp(ctx, claims, &app, appAccessStore) {
			filteredApps = append(filteredApps, app)
		}
	}

	applications := mappers.AppStoreAppsToModels(filteredApps)

	// For each app, fetch its versions and their deployments
	for i := range applications {
		dynamoVersions, err := versionStore.GetByApplicationId(ctx, applications[i].Uuid)
		if err != nil {
			log.Printf("error fetching versions for application %s: %v", applications[i].Uuid, err)
			continue
		}
		versions := mappers.AppStoreVersionsToModels(dynamoVersions)

		// Fetch deployments for each version (keyed by version uuid)
		for j := range versions {
			deployments, err := deploymentsStore.GetHistory(ctx, versions[j].Uuid)
			if err != nil {
				log.Printf("error fetching deployments for version %s: %v", versions[j].Uuid, err)
				continue
			}
			versions[j].Deployments = mappers.DeploymentItemsToModels(deployments)
		}

		applications[i].Versions = versions
		applications[i].LatestVersionTag = latestVersionTag(versions)
	}

	m, err := json.Marshal(applications)
	if err != nil {
		log.Println(err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       handlerError(handlerName, ErrMarshaling),
		}, nil
	}
	return events.APIGatewayV2HTTPResponse{
		StatusCode: http.StatusOK,
		Body:       string(m),
	}, nil
}

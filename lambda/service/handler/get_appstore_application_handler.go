package handler

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/pennsieve/app-deploy-service/service/mappers"
	"github.com/pennsieve/app-deploy-service/service/models"
	"github.com/pennsieve/app-deploy-service/service/store_dynamodb"
	ghsync "github.com/pennsieve/github-client/pkg/github/sync"
	"github.com/pennsieve/pennsieve-go-core/pkg/authorizer"
)

func GetAppstoreApplicationHandler(ctx context.Context, request events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	handlerName := "GetAppstoreApplicationHandler"

	appId := request.PathParameters["id"]
	if appId == "" {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusBadRequest,
			Body:       handlerError(handlerName, ErrMissingParams),
		}, nil
	}

	cfg, err := awsconfig.LoadDefaultConfig(ctx)
	if err != nil {
		log.Println(err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       handlerError(handlerName, ErrConfig),
		}, nil
	}

	dynamoDBClient := dynamodb.NewFromConfig(cfg)
	appStoreStore := store_dynamodb.NewAppStoreDatabaseStore(dynamoDBClient, os.Getenv(appstoreApplicationsTableNameKey))
	versionStore := store_dynamodb.NewAppStoreVersionDatabaseStore(dynamoDBClient, os.Getenv(appstoreVersionsTableNameKey))
	deploymentsStore := store_dynamodb.NewDeploymentsStore(dynamoDBClient, os.Getenv(deploymentsTableNameKey))
	appAccessStore := store_dynamodb.NewAppAccessDatabaseStore(dynamoDBClient, os.Getenv(appAccessTableNameKey))

	app, err := appStoreStore.GetById(ctx, appId)
	if err != nil {
		log.Println(err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       handlerError(handlerName, ErrDynamoDB),
		}, nil
	}
	if app == nil {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusNotFound,
			Body:       handlerError(handlerName, ErrAppNotFound),
		}, nil
	}

	claims := authorizer.ParseClaims(request.RequestContext.Authorizer.Lambda)
	log.Printf("%s: caller org=%s user=%s", handlerName, claims.OrgClaim.NodeId, claims.UserClaim.NodeId)

	if !CanAccessApp(ctx, claims, app, appAccessStore) {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusForbidden,
			Body:       handlerError(handlerName, ErrNotPermitted),
		}, nil
	}

	application := mappers.AppStoreAppToModel(*app)

	dynamoVersions, err := versionStore.GetByApplicationId(ctx, application.Uuid)
	if err != nil {
		log.Printf("error fetching versions for application %s: %v", application.Uuid, err)
	} else {
		versions := mappers.AppStoreVersionsToModels(dynamoVersions)
		for j := range versions {
			deployments, err := deploymentsStore.GetHistory(ctx, versions[j].Uuid)
			if err != nil {
				log.Printf("error fetching deployments for version %s: %v", versions[j].Uuid, err)
				continue
			}
			versions[j].Deployments = mappers.DeploymentItemsToModels(deployments)
		}
		application.Versions = versions
	}

	latestTag := latestVersionTag(application.Versions)
	tag := request.QueryStringParameters["tag"]
	if tag == "" {
		tag = latestTag
	}

	assets := map[string]string{}
	if tag != "" {
		assets = fetchAssets(ctx, cfg, app.SourceUrl, tag)
	}

	detail := models.AppStoreApplicationDetail{
		Uuid:             application.Uuid,
		SourceUrl:        application.SourceUrl,
		SourceType:       application.SourceType,
		IsPrivate:        application.IsPrivate,
		Visibility:       application.Visibility,
		OwnerId:          application.OwnerId,
		CreatedAt:        application.CreatedAt,
		LatestVersionTag: latestTag,
		Versions:         application.Versions,
		Assets:           assets,
	}

	m, err := json.Marshal(detail)
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

// latestVersionTag returns the Version tag of the most recently created version,
// or an empty string if there are no versions with a tag. Assets are only synced
// for release tags, so there is no meaningful default when no release exists.
func latestVersionTag(versions []models.AppStoreVersion) string {
	latest := ""
	latestCreatedAt := ""
	for _, v := range versions {
		if v.Version == "" {
			continue
		}
		if v.CreatedAt > latestCreatedAt {
			latestCreatedAt = v.CreatedAt
			latest = v.Version
		}
	}
	return latest
}

func fetchAssets(ctx context.Context, cfg aws.Config, sourceUrl string, tag string) map[string]string {
	bucket := os.Getenv("CONTENT_SYNC_BUCKET")
	if bucket == "" {
		log.Println("warning: CONTENT_SYNC_BUCKET not set, skipping asset fetch")
		return map[string]string{}
	}

	namespace := buildNamespace(sourceUrl, tag)
	s3Client := s3.NewFromConfig(cfg)
	dest := ghsync.NewS3Destination(s3Client, bucket)

	assets := map[string]string{}
	for _, file := range getSyncFiles() {
		key := namespace + "/" + file
		data, _, err := dest.Read(ctx, key)
		if err != nil {
			log.Printf("asset %s not found: %v", file, err)
			continue
		}
		assets[file] = string(data)
	}

	return assets
}

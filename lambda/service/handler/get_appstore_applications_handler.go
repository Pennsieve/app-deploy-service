package handler

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/pennsieve/app-deploy-service/service/mappers"
	"github.com/pennsieve/app-deploy-service/service/models"
	"github.com/pennsieve/app-deploy-service/service/store_dynamodb"
	"github.com/pennsieve/pennsieve-go-core/pkg/authorizer"
)

// GetAppstoreApplicationsHandler serves GET /store: the apps the caller can
// use in the workspace of the request (organization_id), including apps
// shared with that workspace or the caller's teams in it.
func GetAppstoreApplicationsHandler(ctx context.Context, request events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	return listAppstoreApplications(ctx, request, "GetAppstoreApplicationsHandler", CanAccessApp)
}

// GetMyAppstoreApplicationsHandler serves GET /store/me: the apps
// the caller can use outside any workspace (public, owned, or shared with the
// caller directly).
func GetMyAppstoreApplicationsHandler(ctx context.Context, request events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	return listAppstoreApplications(ctx, request, "GetMyAppstoreApplicationsHandler", CanAccessAppPersonally)
}

// listConcurrency bounds the apps worked on at once when listing.
const listConcurrency = 10

type appAccessCheck func(context.Context, *authorizer.Claims, *store_dynamodb.AppStoreApplication, *store_dynamodb.AppAccessDatabaseStore) bool

func listAppstoreApplications(ctx context.Context, request events.APIGatewayV2HTTPRequest, handlerName string, canAccess appAccessCheck) (events.APIGatewayV2HTTPResponse, error) {

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
	callerOrg, callerUser := "", ""
	if claims.OrgClaim != nil {
		callerOrg = claims.OrgClaim.NodeId
	}
	if claims.UserClaim != nil {
		callerUser = claims.UserClaim.NodeId
	}
	log.Printf("%s: caller org=%s user=%s", handlerName, callerOrg, callerUser)

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

	includeArchived := false
	if v, found := queryParams["includeArchived"]; found {
		if parsed, parseErr := strconv.ParseBool(v); parseErr == nil {
			includeArchived = parsed
		}
	}

	// Deployment histories only on request (?include=deployments): the
	// Apps tab doesn't show them, and they grow with every release.
	includeDeployments := strings.Contains(queryParams["include"], "deployments")

	var candidates []store_dynamodb.AppStoreApplication
	for _, app := range dynamoApps {
		if !includeArchived && app.Status == models.AppStoreStatusArchived {
			continue
		}
		candidates = append(candidates, app)
	}

	// Per app, in parallel (a bounded pool): the access check, its versions,
	// optionally their deployments, and its README summary when the latest
	// release hasn't been summarized yet. Done one after another, this was
	// hundreds of DynamoDB round trips in a row.
	type listed struct {
		ok  bool
		app models.AppStoreApplication
	}
	results := make([]listed, len(candidates))
	sem := make(chan struct{}, listConcurrency)
	var wg sync.WaitGroup
	for i := range candidates {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()
			app := candidates[i]
			if !canAccess(ctx, claims, &app, appAccessStore) {
				return
			}
			out := mappers.AppStoreAppToModel(app)
			dynamoVersions, err := versionStore.GetByApplicationId(ctx, app.Uuid)
			if err != nil {
				log.Printf("error fetching versions for application %s: %v", app.Uuid, err)
				results[i] = listed{ok: true, app: out}
				return
			}
			versions := mappers.AppStoreVersionsToModels(dynamoVersions)
			if includeDeployments {
				for j := range versions {
					deployments, err := deploymentsStore.GetHistory(ctx, versions[j].Uuid)
					if err != nil {
						log.Printf("error fetching deployments for version %s: %v", versions[j].Uuid, err)
						continue
					}
					versions[j].Deployments = mappers.DeploymentItemsToModels(deployments)
				}
			}
			out.Versions = versions
			out.LatestVersionTag = latestVersionTag(versions)
			// Apps released before summaries existed: summarize once, store.
			if out.LatestVersionTag != "" && app.SummaryTag != out.LatestVersionTag {
				out.Summary = syncedReadmeSummary(ctx, cfg, app.SourceUrl, out.LatestVersionTag)
				if err := appStoreStore.UpdateSummary(ctx, app.Uuid, out.Summary, out.LatestVersionTag); err != nil {
					log.Printf("warning: unable to store the README summary of %s: %v", app.Uuid, err)
				}
			}
			results[i] = listed{ok: true, app: out}
		}(i)
	}
	wg.Wait()

	applications := []models.AppStoreApplication{}
	for _, r := range results {
		if r.ok {
			applications = append(applications, r.app)
		}
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

package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/pennsieve/app-deploy-service/service/manifestschema"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	"github.com/aws/aws-sdk-go-v2/service/ecs/types"
	"github.com/google/uuid"
	"github.com/pennsieve/app-deploy-service/service/models"
	"github.com/pennsieve/app-deploy-service/service/runner"
	"github.com/pennsieve/app-deploy-service/service/store_dynamodb"
	"github.com/pennsieve/pennsieve-go-core/pkg/authorizer"
	"github.com/pennsieve/pennsieve-go-core/pkg/models/role"
)

func PostAppStoreHandler(ctx context.Context, request events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	handlerName := "PostAppStoreHandler"
	var application models.AppStoreDeployment
	if err := json.Unmarshal([]byte(request.Body), &application); err != nil {
		log.Println(err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       handlerError(handlerName, ErrUnmarshaling),
		}, nil
	}

	envValue := os.Getenv("ENV")

	TaskDefinitionArn := os.Getenv("TASK_DEF_ARN")
	DeployerTaskDefinitionArn := os.Getenv("DEPLOYER_TASK_DEF_ARN")
	subIdStr := os.Getenv("SUBNET_IDS")
	SubNetIds := strings.Split(subIdStr, ",")
	cluster := os.Getenv("CLUSTER_ARN")
	SecurityGroup := os.Getenv("SECURITY_GROUP")
	TaskDefContainerName := os.Getenv("TASK_DEF_CONTAINER_NAME")
	DeployerTaskDefContainerName := os.Getenv("DEPLOYER_TASK_DEF_CONTAINER_NAME")
	deploymentsTable := os.Getenv(deploymentsTableNameKey)
	applicationsTable := os.Getenv(appstoreApplicationsTableNameKey)
	versionsTable := os.Getenv(appstoreVersionsTableNameKey)
	deploymentId := uuid.NewString()
	actionKey := "ACTION"
	actionValue := "ADD_TO_APPSTORE"

	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		log.Println(err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       handlerError(handlerName, ErrConfig),
		}, nil
	}

	var userId string
	if request.RequestContext.Authorizer != nil && request.RequestContext.Authorizer.Lambda != nil {
		claims := authorizer.ParseClaims(request.RequestContext.Authorizer.Lambda)
		if !authorizer.HasOrgRole(claims, role.Viewer) {
			log.Printf("user not permitted to add to appstore with claims: %+v", claims)
			return events.APIGatewayV2HTTPResponse{
				StatusCode: http.StatusUnauthorized,
				Body:       handlerError(handlerName, ErrNotPermitted),
			}, nil
		}
		userId = claims.UserClaim.NodeId
	} else {
		log.Println("direct invocation detected, skipping authorization")
		userId = application.Source.Owner
	}

	dynamoDBClient := dynamodb.NewFromConfig(cfg)
	appStoreStore := store_dynamodb.NewAppStoreDatabaseStore(dynamoDBClient, applicationsTable)
	versionStore := store_dynamodb.NewAppStoreVersionDatabaseStore(dynamoDBClient, versionsTable)
	deploymentsStore := store_dynamodb.NewDeploymentsStore(dynamoDBClient, deploymentsTable)

	// Check if app exists by sourceUrl; create if not
	var applicationId string
	existingApps, err := appStoreStore.GetBySourceUrl(ctx, application.Source.Url)
	if err != nil {
		log.Println(err)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: 500,
			Body:       handlerError(handlerName, ErrDynamoDB),
		}, nil
	}

	if len(existingApps) > 0 {
		applicationId = existingApps[0].Uuid
		log.Printf("application %s already exists for sourceUrl %s", applicationId, application.Source.Url)

		newVisibility := application.Source.IsPrivate
		currentGithubVisibility := existingApps[0].IsPrivate
		if currentGithubVisibility != newVisibility {
			log.Printf("updating visibility for application %s from %v to %v", applicationId, currentGithubVisibility, newVisibility)
			if err := appStoreStore.UpdateGithubVisibility(ctx, applicationId, newVisibility); err != nil {
				log.Println("error updating isPrivate: ", err.Error())
				return events.APIGatewayV2HTTPResponse{
					StatusCode: http.StatusInternalServerError,
					Body:       handlerError(handlerName, ErrPublishingToAppStore),
				}, nil
			}
		}
		if existingApps[0].Status == models.AppStoreStatusArchived {
			log.Printf("application %s for sourceUrl %s is in status %s, cannot publish to AppStore",
				applicationId, application.Source.Url, existingApps[0].Status)
			return events.APIGatewayV2HTTPResponse{
				StatusCode: http.StatusUnprocessableEntity,
				Body:       handlerError(handlerName, ErrPublishingToAppStore),
			}, nil
		}
	} else {
		applicationId = uuid.NewString()
		visibility := initialVisibility(application.Source.IsPrivate, application.Access)
		appRecord := store_dynamodb.AppStoreApplication{
			Uuid:       applicationId,
			SourceUrl:  application.Source.Url,
			SourceType: application.Source.SourceType,
			IsPrivate:  application.Source.IsPrivate,
			Visibility: visibility,
			OwnerId:    userId,
			CreatedAt:  time.Now().UTC().String(),
			Status:     models.AppStoreStatusActive,
		}
		if err := appStoreStore.Insert(ctx, appRecord); err != nil {
			log.Println("error inserting appstore application: ", err.Error())
			return events.APIGatewayV2HTTPResponse{
				StatusCode: http.StatusInternalServerError,
				Body:       handlerError(handlerName, ErrStoringApplication),
			}, nil
		}

		// the owner row, plus the grants the publisher chose up front (if any);
		// a request without access gives the owner-only list
		appAccessTable := os.Getenv(appAccessTableNameKey)
		appAccessStore := store_dynamodb.NewAppAccessDatabaseStore(dynamoDBClient, appAccessTable)
		var initialAccess models.SetPermissionsRequest
		if application.Access != nil {
			initialAccess = *application.Access
		}
		for _, entry := range accessEntriesFor(applicationId, userId, userId, time.Now().UTC().String(), initialAccess) {
			if err := appAccessStore.Insert(ctx, entry); err != nil {
				log.Printf("error inserting %s access for %s: %s", entry.EntityType, entry.EntityId, err.Error())
			}
		}
		log.Printf("application %s starts %s with %d access entries", applicationId, visibility, len(accessEntriesFor(applicationId, userId, userId, "", initialAccess)))

		log.Printf("created new appstore application %s for sourceUrl %s", applicationId, application.Source.Url)
	}

	// The commit the tag points to now, so a review of this version is tied
	// to the code that was built (a tag can be moved later). A failed lookup
	// only logs.
	commitSha, err := releaseCommit(ctx, application.Source.Url, application.Source.Tag, application.Source.AuthToken)
	if err != nil {
		log.Printf("warning: unable to resolve the commit of %s %s: %v", application.Source.Url, application.Source.Tag, err)
	}

	// Always create a new version entry
	versionUuid := uuid.NewString()
	versionRecord := store_dynamodb.AppStoreVersion{
		Uuid:          versionUuid,
		ApplicationId: applicationId,
		Version:       application.Source.Tag,
		ReleaseId:     application.Release.ID,
		CreatedAt:     time.Now().UTC().String(),
		Status:        "registering",
		CommitSha:     commitSha,
	}
	if err := versionStore.Insert(ctx, versionRecord); err != nil {
		log.Println("error inserting appstore version: ", err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       handlerError(handlerName, ErrStoringApplication),
		}, nil
	}

	syncRepoContent(ctx, application.Source.Url, application.Source.Tag, application.Source.AuthToken)

	// The README's first paragraph, for the app's card in lists (best effort).
	if err := appStoreStore.UpdateSummary(ctx, applicationId, syncedReadmeSummary(ctx, cfg, application.Source.Url, application.Source.Tag), application.Source.Tag); err != nil {
		log.Printf("warning: unable to store the README summary of %s: %v", application.Source.Url, err)
	}

	// Read the synced app.yml once and reuse it for build sizing and parameter
	// persistence. A missing or malformed app.yml yields haveAppCfg=false so
	// neither step blocks the deployment.
	appYml, haveAppYml := readSyncedAppYml(ctx, cfg, application.Source.Url, application.Source.Tag)
	var appCfg appConfig
	haveAppCfg := false
	var problems []string
	if haveAppYml {
		// Checked against its schema (manifestschema): recorded on the version
		// in "warn" mode, the release refused in "enforce" mode.
		problems = manifestProblems(appYml)
		if c, err := parseAppConfig(appYml); err == nil {
			appCfg, haveAppCfg = c, true
		} else {
			log.Printf("warning: unable to parse the app.yml of %s %s: %v", application.Source.Url, application.Source.Tag, err)
		}
	}
	// "--" separates the release tag from the variant id in image tags, so
	// an app with variants can't use it in a tag, whatever the validation mode.
	if haveAppCfg {
		if reason := variantTagProblem(appCfg, application.Source.Tag); reason != "" {
			log.Printf("refusing %s %s: %s", application.Source.Url, application.Source.Tag, reason)
			refusal := withLiveUpdates(ctx, NewAppStoreStatusManager(handlerName, versionStore, versionUuid), cfg)
			return events.APIGatewayV2HTTPResponse{
				StatusCode: http.StatusUnprocessableEntity,
				Body:       refusal.SetErrorStatus(ctx, fmt.Errorf("%s", reason)),
			}, nil
		}
	}
	// app.yml's variants, resolved once: what the version records, which
	// images the release builds and how. With APP_VARIANTS_ENABLED=false
	// only the default variant is built.
	var resolved resolvedApp
	if haveAppCfg {
		resolved = resolveVariants(appCfg)
	}
	buildAllVariants := variantBuildsEnabled()
	if len(problems) > 0 {
		log.Printf("app.yml of %s %s doesn't follow %s: %s", application.Source.Url, application.Source.Tag, manifestschema.URL, strings.Join(problems, "; "))
		if manifestValidationMode() == manifestValidationEnforce {
			if err := versionStore.SetRegistrationDetails(ctx, versionUuid, "", problems); err != nil {
				log.Printf("warning: unable to record the manifest problems of %s: %v", versionUuid, err)
			}
			refusal := withLiveUpdates(ctx, NewAppStoreStatusManager(handlerName, versionStore, versionUuid), cfg)
			return events.APIGatewayV2HTTPResponse{
				StatusCode: http.StatusUnprocessableEntity,
				Body: refusal.SetErrorStatus(ctx, fmt.Errorf("app.yml doesn't follow %s: %s",
					manifestschema.URL, strings.Join(problems, "; "))),
			}, nil
		}
	}

	// While kaniko builds the images, a Dockerfile it can't build faithfully
	// (heredocs, RUN --mount) is refused here with the reason, instead of
	// silently pushing an image other than the one the author tested.
	// Skipped when the environment builds with BuildKit (IMAGE_BUILDER).
	var dockerfiles []string
	if haveAppCfg {
		dockerfiles = builtDockerfiles(resolved, buildAllVariants)
	}
	if reason := unsupportedDockerfileSyntax(application.Source.Url, application.Source.Tag, application.Source.AuthToken, dockerfiles); reason != "" {
		log.Printf("refusing %s %s: %s", application.Source.Url, application.Source.Tag, reason)
		refusal := withLiveUpdates(ctx, NewAppStoreStatusManager(handlerName, versionStore, versionUuid), cfg)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusUnprocessableEntity,
			Body:       refusal.SetErrorStatus(ctx, errors.New(reason)),
		}, nil
	}

	// The repository's CITATION.cff at this tag, typed by app.yml's
	// application.relations. An invalid file, or a relation for a DOI it
	// doesn't list, is a manifest problem; it never refuses the release.
	cff, haveCFF := readSyncedFile(ctx, cfg, application.Source.Url, application.Source.Tag, citationFile)
	appCite, citeProblems := appCitation(cff, haveCFF, appCfg.Application.Relations)
	if len(citeProblems) > 0 {
		log.Printf("citation of %s %s: %s", application.Source.Url, application.Source.Tag, strings.Join(citeProblems, "; "))
		problems = append(problems, citeProblems...)
	}
	if appCite != nil {
		if err := versionStore.SetCitation(ctx, versionUuid, appCite); err != nil {
			log.Printf("warning: unable to persist the citation of %s %s: %v", applicationId, application.Source.Tag, err)
		}
	}

	// The repository's license at this tag (from its LICENSE file, as GitHub
	// reads it), kept with the version. A failed lookup only logs.
	license, err := repoLicense(ctx, application.Source.Url, application.Source.Tag, application.Source.AuthToken)
	if err != nil {
		log.Printf("warning: unable to read the license of %s %s: %v", application.Source.Url, application.Source.Tag, err)
	}
	if license != "" || len(problems) > 0 {
		if err := versionStore.SetRegistrationDetails(ctx, versionUuid, license, problems); err != nil {
			log.Printf("warning: unable to record the license and manifest problems of %s: %v", versionUuid, err)
		}
	}

	// Record the version's app.yml on the version, and, when this is the
	// app's newest version, its parameters on the app (what consumers of the
	// app's top-level params read). Registering an older tag again doesn't
	// roll the app back. Advisory: a failure here never blocks the deployment,
	// and an app without an app.yml simply has no manifest. With variants,
	// the manifest and params are the default variant's, and the variants
	// (each with its resolved manifest and build settings) are recorded
	// beside them; without, the version record is as it always was.
	//
	// Each variant that is built gets its own deployment (the default's is
	// this release's); the provisioner reads the variants and build settings
	// back from the version (READ_VERSION_BUILDS) and starts one build each.
	var variantDeployments []variantDeployment
	readVersionBuilds := false
	if haveAppCfg {
		if err := versionStore.SetManifest(ctx, versionUuid, resolved.versionManifest()); err != nil {
			log.Printf("warning: unable to persist the manifest of %s %s: %v", applicationId, application.Source.Tag, err)
		}
		// app.yml's review block, for the review of this release
		if review := appReviewRecord(appCfg.Review); review != nil {
			if err := versionStore.SetReview(ctx, versionUuid, review); err != nil {
				log.Printf("warning: unable to persist the review block of %s %s: %v", applicationId, application.Source.Tag, err)
			}
		}
		if vv := resolved.versionVariants(); !vv.Empty() {
			planned := planVariantBuilds(&vv, resolved, deploymentId, buildAllVariants, uuid.NewString)
			if err := versionStore.SetVariants(ctx, versionUuid, vv); err != nil {
				// the provisioner then builds the one image, as it would
				// without variants
				log.Printf("warning: unable to persist the variants of %s %s: %v", applicationId, application.Source.Tag, err)
			} else {
				variantDeployments, readVersionBuilds = planned, true
			}
		}
		if isNewestVersion(ctx, versionStore, applicationId, application.Source.Tag) {
			if err := appStoreStore.UpdateParams(ctx, applicationId, resolved.parameters()); err != nil {
				log.Printf("warning: unable to persist app parameters for %s: %v", applicationId, err)
			}
		}
	}

	// Derive build-time ephemeral storage from app.yml (e.g. GPU apps pull large
	// dependencies that exhaust Fargate's default 20 GiB). 0 means no override.
	// With variants, this is the default variant's; each variant's own is
	// recorded on it (planVariantBuilds).
	buildStorage := int32(0)
	if haveAppCfg {
		buildStorage = buildStorageGiB(resolved.Default)
	}

	// StatusManager uses the version store for status updates (keyed by versionUuid)
	statusManager := NewAppStoreStatusManager(handlerName, versionStore, versionUuid).
		WithDeployment(deploymentsStore, deploymentId)

	// live status updates: AppSync Events where configured, otherwise Pusher
	statusManager = withLiveUpdates(ctx, statusManager, cfg)

	// Create deployment record (applicationId = versionUuid for tracking)
	if err := statusManager.NewDeployment(ctx, store_dynamodb.Deployment{
		DeploymentKey: store_dynamodb.DeploymentKey{
			DeploymentId:  deploymentId,
			ApplicationId: versionUuid,
		},
		ReleaseId:       application.Release.ID,
		InitiatedAt:     time.Now().UTC(),
		WorkspaceNodeId: appstoreIdentifier,
		UserNodeId:      application.Source.SourceType,
		Action:          actionValue,
		LastStatus:      "NOT_STARTED",
		SourceUrl:       application.Source.Url,
		Tag:             application.Source.Tag,
		Variant:         resolved.defaultVariantBuilt(variantDeployments),
	}); err != nil {
		log.Println("error creating deployment record: ", err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       handlerError(handlerName, ErrStoringDeployment),
		}, nil
	}
	// one more deployment per other variant built, which the status lambda
	// follows like the release's own
	for _, vd := range variantDeployments {
		if vd.DeploymentId == deploymentId {
			continue
		}
		if err := deploymentsStore.Insert(ctx, store_dynamodb.Deployment{
			DeploymentKey: store_dynamodb.DeploymentKey{
				DeploymentId:  vd.DeploymentId,
				ApplicationId: versionUuid,
			},
			ReleaseId:       application.Release.ID,
			InitiatedAt:     time.Now().UTC(),
			WorkspaceNodeId: appstoreIdentifier,
			UserNodeId:      application.Source.SourceType,
			Action:          actionValue,
			LastStatus:      "NOT_STARTED",
			SourceUrl:       application.Source.Url,
			Tag:             application.Source.Tag,
			Variant:         vd.Variant,
		}); err != nil {
			log.Printf("error creating the deployment record of variant %s: %s", vd.Variant, err.Error())
			return events.APIGatewayV2HTTPResponse{
				StatusCode: http.StatusInternalServerError,
				Body:       statusManager.SetErrorStatus(ctx, ErrStoringDeployment),
			}, nil
		}
	}

	log.Println("Initiating new AppStore Fargate Task.")
	client := ecs.NewFromConfig(cfg)
	envKey := "ENV"

	sourceTypeKey := "SOURCE_TYPE"
	sourceTypeValue := application.Source.SourceType
	sourceTagKey := "SOURCE_TAG"
	sourceTagValue := application.Source.Tag
	sourceUrlKey := "SOURCE_URL"
	sourceUrlValue := application.Source.Url

	deployerTaskDefnKey := "DEPLOYER_TASK_DEF_ARN"
	deployerTaskDefnValue := DeployerTaskDefinitionArn
	subetsIdKey := "SUBNET_IDS"
	subetsIdValue := subIdStr
	clusterKey := "CLUSTER_ARN"
	clusterValue := cluster
	securityGroupKey := "SECURITY_GROUP"
	securityGroupValue := SecurityGroup
	deployertaskDefnContainerKey := "DEPLOYER_TASK_DEF_CONTAINER_NAME"
	deployertaskDefnContainerValue := DeployerTaskDefContainerName

	authTokenKey := "AUTH_TOKEN"
	authTokenValue := application.Source.AuthToken

	environment := []types.KeyValuePair{
		{
			Name:  aws.String(applicationUuidKey),
			Value: aws.String(versionUuid),
		},
		{
			Name:  aws.String(deploymentIdKey),
			Value: aws.String(deploymentId),
		},
		{
			Name:  &envKey,
			Value: &envValue,
		},
		{
			Name:  &actionKey,
			Value: &actionValue,
		},
		{
			Name:  &sourceTypeKey,
			Value: &sourceTypeValue,
		},
		{
			Name:  &sourceUrlKey,
			Value: &sourceUrlValue,
		},
		{
			Name:  &sourceTagKey,
			Value: &sourceTagValue,
		},
		{
			Name:  &deployerTaskDefnKey,
			Value: &deployerTaskDefnValue,
		},
		{
			Name:  &subetsIdKey,
			Value: &subetsIdValue,
		},
		{
			Name:  &clusterKey,
			Value: &clusterValue,
		},
		{
			Name:  &securityGroupKey,
			Value: &securityGroupValue,
		},
		{
			Name:  &deployertaskDefnContainerKey,
			Value: &deployertaskDefnContainerValue,
		},
		{
			Name:  aws.String(deploymentsTableNameKey),
			Value: aws.String(deploymentsTable),
		},
		{
			// Fargate uses APPLICATIONS_TABLE to update the version record
			Name:  aws.String(applicationsTableNameKey),
			Value: aws.String(versionsTable),
		},
		{
			Name:  &authTokenKey,
			Value: &authTokenValue,
		},
	}

	if readVersionBuilds {
		environment = append(environment, types.KeyValuePair{
			Name:  aws.String(readVersionBuildsEnv),
			Value: aws.String("true"),
		})
	}

	if buildStorage > 0 {
		environment = append(environment, types.KeyValuePair{
			Name:  aws.String("APP_STORAGE"),
			Value: aws.String(strconv.Itoa(int(buildStorage))),
		})
	}

	runTaskIn := &ecs.RunTaskInput{
		TaskDefinition: aws.String(TaskDefinitionArn),
		Cluster:        aws.String(cluster),
		NetworkConfiguration: &types.NetworkConfiguration{
			AwsvpcConfiguration: &types.AwsVpcConfiguration{
				Subnets:        SubNetIds,
				SecurityGroups: []string{SecurityGroup},
				AssignPublicIp: types.AssignPublicIpEnabled,
			},
		},
		Overrides: &types.TaskOverride{
			ContainerOverrides: []types.ContainerOverride{
				{
					Name:        &TaskDefContainerName,
					Environment: environment,
				},
			},
		},
		LaunchType: types.LaunchTypeFargate,
		Tags: []types.Tag{
			{Key: aws.String(deploymentIdTag), Value: aws.String(deploymentId)},
			{Key: aws.String(applicationIdTag), Value: aws.String(versionUuid)},
			// Points status lambda at the versions table
			{Key: aws.String("ApplicationsTable"), Value: aws.String(versionsTable)},
		},
	}

	taskRunner := runner.NewECSTaskRunner(client, runTaskIn)
	runTaskOut, err := taskRunner.Run(ctx)
	if err != nil {
		log.Println("error running task: ", err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: 500,
			Body:       statusManager.SetErrorStatus(ctx, ErrRunningFargateTask),
		}, nil
	}
	if err := runner.GetRunFailures(runTaskOut); err != nil {
		log.Println("run failures from task: ", err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: 500,
			Body:       statusManager.SetErrorStatus(ctx, ErrRunningFargateTask),
		}, nil
	}
	if len(runTaskOut.Tasks) > 0 {
		log.Printf("started Add to AppStore deployment %s of version %s (tag %s) from %s in task %s",
			deploymentId,
			versionUuid,
			application.Source.Tag,
			application.Source.Url,
			aws.ToString(runTaskOut.Tasks[0].TaskArn))
	}

	m, err := json.Marshal(models.DeployApplicationResponse{DeploymentId: deploymentId})
	if err != nil {
		log.Println("error marshalling response: ", err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: 500,
			Body:       handlerError(handlerName, ErrMarshaling),
		}, nil
	}

	return events.APIGatewayV2HTTPResponse{
		StatusCode: http.StatusAccepted,
		Body:       string(m),
	}, nil
}

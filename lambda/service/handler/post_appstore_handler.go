package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	"github.com/aws/aws-sdk-go-v2/service/ecs/types"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/google/uuid"
	"github.com/pennsieve/app-deploy-service/service/models"
	"github.com/pennsieve/app-deploy-service/service/runner"
	"github.com/pennsieve/app-deploy-service/service/store_dynamodb"
	"github.com/pennsieve/pennsieve-go-core/pkg/authorizer"
	"github.com/pennsieve/pennsieve-go-core/pkg/models/role"
	"github.com/pusher/pusher-http-go/v5"
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
	if request.RequestContext.Authorizer.Lambda != nil {
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
		userId = "system"
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
	} else {
		applicationId = uuid.NewString()
		visibility := "public"
		if application.Source.IsPrivate {
			visibility = "private"
		}
		appRecord := store_dynamodb.AppStoreApplication{
			Uuid:       applicationId,
			SourceUrl:  application.Source.Url,
			SourceType: application.Source.SourceType,
			IsPrivate:  application.Source.IsPrivate,
			Visibility: visibility,
			OwnerId:    userId,
			CreatedAt:  time.Now().UTC().String(),
		}
		if err := appStoreStore.Insert(ctx, appRecord); err != nil {
			log.Println("error inserting appstore application: ", err.Error())
			return events.APIGatewayV2HTTPResponse{
				StatusCode: http.StatusInternalServerError,
				Body:       handlerError(handlerName, ErrStoringApplication),
			}, nil
		}

		appAccessTable := os.Getenv(appAccessTableNameKey)
		appAccessStore := store_dynamodb.NewAppAccessDatabaseStore(dynamoDBClient, appAccessTable)
		ownerAccess := store_dynamodb.AppAccess{
			EntityId:    fmt.Sprintf("user#%s", userId),
			AppId:       fmt.Sprintf("app#%s", applicationId),
			EntityType:  "user",
			EntityRawId: userId,
			AppUuid:     applicationId,
			AccessType:  "owner",
			GrantedAt:   time.Now().UTC().String(),
			GrantedBy:   userId,
		}
		if err := appAccessStore.Insert(ctx, ownerAccess); err != nil {
			log.Println("error inserting owner access: ", err.Error())
		}

		log.Printf("created new appstore application %s for sourceUrl %s", applicationId, application.Source.Url)
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
	}
	if err := versionStore.Insert(ctx, versionRecord); err != nil {
		log.Println("error inserting appstore version: ", err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       handlerError(handlerName, ErrStoringApplication),
		}, nil
	}

	syncRepoContent(ctx, application.Source.Url, application.Source.Tag, application.Source.AuthToken)

	// StatusManager uses the version store for status updates (keyed by versionUuid)
	statusManager := NewAppStoreStatusManager(handlerName, versionStore, versionUuid).
		WithDeployment(deploymentsStore, deploymentId)

	// Add pusher for real-time updates
	ssmClient := ssm.NewFromConfig(cfg)
	if pusherConfig, err := GetPusherConfig(ctx, ssmClient); err != nil {
		log.Printf("warning: %v\n", err)
	} else {
		statusManager = statusManager.WithPusher(&pusher.Client{
			AppID:   pusherConfig.AppId,
			Key:     pusherConfig.Key,
			Secret:  pusherConfig.Secret,
			Cluster: pusherConfig.Cluster,
			Secure:  true,
		})
	}

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
	}); err != nil {
		log.Println("error creating deployment record: ", err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       handlerError(handlerName, ErrStoringDeployment),
		}, nil
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
					Name: &TaskDefContainerName,
					Environment: []types.KeyValuePair{
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
					},
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

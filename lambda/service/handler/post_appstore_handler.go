package handler

import (
	"context"
	"encoding/json"
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
	"github.com/pusher/pusher-http-go/v5"
)

func PostAppStoreHandler(ctx context.Context, request events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	handlerName := "PostAppStoreHandler"
	var application models.Application
	if err := json.Unmarshal([]byte(request.Body), &application); err != nil {
		log.Println(err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       handlerError(handlerName, ErrUnmarshaling),
		}, nil
	}

	envValue := os.Getenv("ENV")
	if application.Env != "" {
		envValue = application.Env
	}

	TaskDefinitionArn := os.Getenv("TASK_DEF_ARN")
	DeployerTaskDefinitionArn := os.Getenv("DEPLOYER_TASK_DEF_ARN")
	subIdStr := os.Getenv("SUBNET_IDS")
	SubNetIds := strings.Split(subIdStr, ",")
	cluster := os.Getenv("CLUSTER_ARN")
	SecurityGroup := os.Getenv("SECURITY_GROUP")
	TaskDefContainerName := os.Getenv("TASK_DEF_CONTAINER_NAME")
	DeployerTaskDefContainerName := os.Getenv("DEPLOYER_TASK_DEF_CONTAINER_NAME")
	deploymentsTable := os.Getenv(deploymentsTableNameKey)
	applicationsTable := os.Getenv("APPLICATIONS_TABLE")
	var applicationUuid string
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

	// TODO: add authorization and authentication
	// claims := authorizer.ParseClaims(request.RequestContext.Authorizer.Lambda)
	// // Maybe we should check for role.Writer instead here, but I'm not
	// // sure if there is a difference for org roles.
	// // So just making sure the user is not a guest
	// if !authorizer.HasOrgRole(claims, role.Viewer) {
	// 	log.Printf("user not permitted to deploy application with claims: %+v", claims)
	// 	return events.APIGatewayV2HTTPResponse{
	// 		StatusCode: http.StatusUnauthorized,
	// 		Body:       handlerError(handlerName, ErrNotPermitted),
	// 	}, nil
	// }
	// organizationId := claims.OrgClaim.NodeId
	// userId := claims.UserClaim.NodeId

	client := ecs.NewFromConfig(cfg)
	dynamoDBClient := dynamodb.NewFromConfig(cfg)
	applicationsStore := store_dynamodb.NewApplicationDatabaseStore(dynamoDBClient, applicationsTable)
	deploymentsStore := store_dynamodb.NewDeploymentsStore(dynamoDBClient, deploymentsTable)

	params := map[string]string{
		"sourceUrl": application.Source.Url,
	}
	applications, err := applicationsStore.Get(ctx, appstoreIdentifier, params)
	if err != nil {
		log.Println(err)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: 500,
			Body:       handlerError(handlerName, ErrDynamoDB),
		}, nil
	}

	var statusManager *StatusManager
	if len(applications) > 0 {
		log.Printf("application with sourceUrl %s already exists in AppStore", application.Source.Url)
		applicationUuid = applications[0].Uuid

		statusManager = NewStatusManager(handlerName, applicationsStore, applicationUuid).
			WithDeployment(deploymentsStore, deploymentId)

	} else {
		log.Println("Creating new application record for AppStore deployment.")
		// Persist minimal application record for appstore deployment
		// Note: DestinationUrl will be set by the AddToAppstore function after repository creation
		applicationUuid = uuid.NewString()
		store_application := store_dynamodb.Application{
			Uuid:            applicationUuid,
			SourceType:      application.Source.SourceType,
			SourceUrl:       application.Source.Url,
			ApplicationType: "processor",
			ComputeNodeUuid: appstoreIdentifier,
			OrganizationId:  appstoreIdentifier,
			UserId:          application.Source.SourceType,
			CreatedAt:       time.Now().UTC().String(),
			Status:          "registering",
		}
		statusManager = NewStatusManager(handlerName, applicationsStore, applicationUuid).
			WithDeployment(deploymentsStore, deploymentId)

		err = statusManager.NewApplication(ctx, store_application)
		if err != nil {
			log.Println("error inserting application: ", err.Error())
			return events.APIGatewayV2HTTPResponse{
				StatusCode: http.StatusInternalServerError,
				Body:       handlerError(handlerName, ErrStoringApplication),
			}, nil
		}
	}

	// Add pusher to statusManager if possible for real-time updates
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

	// Create deployment record before launching task
	if err := statusManager.NewDeployment(ctx, store_dynamodb.Deployment{
		DeploymentKey: store_dynamodb.DeploymentKey{
			DeploymentId:  deploymentId,
			ApplicationId: applicationUuid,
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
	envKey := "ENV"
	accountIdKey := "ACCOUNT_ID"
	accountIdValue := application.Account.AccountId
	accountTypeKey := "ACCOUNT_TYPE"
	accountTypeValue := application.Account.AccountType
	accountUuidKey := "ACCOUNT_UUID"
	accountUuidValue := application.Account.Uuid

	sourceTypeKey := "SOURCE_TYPE"
	sourceTypeValue := application.Source.SourceType
	sourceTagKey := "SOURCE_TAG"
	sourceTagValue := application.Source.Tag
	sourceUrlKey := "SOURCE_URL"
	sourceUrlValue := application.Source.Url

	destinationTypeKey := "DESTINATION_TYPE"
	destinationTypeValue := application.Destination.DestinationType
	destinationUrlKey := "DESTINATION_URL"
	destinationUrlValue := application.Destination.Url

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
							Value: aws.String(applicationUuid),
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
							Name:  &accountIdKey,
							Value: &accountIdValue,
						},
						{
							Name:  &accountUuidKey,
							Value: &accountUuidValue,
						},
						{
							Name:  &accountTypeKey,
							Value: &accountTypeValue,
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
							Name:  &destinationTypeKey,
							Value: &destinationTypeValue,
						},
						{
							Name:  &destinationUrlKey,
							Value: &destinationUrlValue,
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
							Name:  aws.String(applicationsTableNameKey),
							Value: aws.String(applicationsTable),
						},
					},
				},
			},
		},
		LaunchType: types.LaunchTypeFargate,
		Tags: []types.Tag{
			{Key: aws.String(deploymentIdTag), Value: aws.String(deploymentId)},
			{Key: aws.String(applicationIdTag), Value: aws.String(applicationUuid)},
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
		// assuming here that if there were failures, then no tasks started.
		// seems safe since for now we are only starting one task
		return events.APIGatewayV2HTTPResponse{
			StatusCode: 500,
			Body:       statusManager.SetErrorStatus(ctx, ErrRunningFargateTask),
		}, nil
	}
	// we expect one task
	if len(runTaskOut.Tasks) > 0 {
		log.Printf("started Add to AppStore deployment %s of application %s from %s in task %s",
			deploymentId,
			applicationUuid,
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

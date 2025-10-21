package handler

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	"github.com/aws/aws-sdk-go-v2/service/ecs/types"
	"github.com/google/uuid"
	"github.com/pennsieve/app-deploy-service/service/models"
	"github.com/pennsieve/app-deploy-service/service/runner"
	"github.com/pennsieve/app-deploy-service/service/utils"
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
	log.Println("Initiating new AppStore Fargate Task.")
	envKey := "ENV"
	accountIdKey := "ACCOUNT_ID"
	accountIdValue := application.Account.AccountId
	accountTypeKey := "ACCOUNT_TYPE"
	accountTypeValue := application.Account.AccountType
	accountUuidKey := "ACCOUNT_UUID"
	accountUuidValue := application.Account.Uuid
	actionKey := "ACTION"
	actionValue := "ADD_TO_APPSTORE"

	sourceTypeKey := "SOURCE_TYPE"
	sourceTypeValue := application.Source.SourceType
	sourceTagKey := "SOURCE_TAG"
	sourceTagValue := application.Source.Tag
	sourceUrlKey := "SOURCE_URL"
	sourceUrlValue, err := utils.DetermineSourceURL(application.Source.Url, sourceTagValue)
	if err != nil {
		log.Println("error determining sourceUrlValue: ", err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusBadRequest,
			Body:       handlerError(handlerName, ErrSourceURL),
		}, nil
	}

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
	registrationId := uuid.NewString()

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
					},
				},
			},
		},
		LaunchType: types.LaunchTypeFargate,
	}

	taskRunner := runner.NewECSTaskRunner(client, runTaskIn)
	runTaskOut, err := taskRunner.Run(ctx)
	if err != nil {
		log.Println("error running task: ", err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: 500,
			Body:       handlerError(handlerName, ErrRunningFargateTask),
		}, nil
	}
	if err := runner.GetRunFailures(runTaskOut); err != nil {
		log.Println("run failures from task: ", err.Error())
		// assuming here that if there were failures, then no tasks started.
		// seems safe since for now we are only starting one task
		return events.APIGatewayV2HTTPResponse{
			StatusCode: 500,
			Body:       handlerError(handlerName, ErrRunningFargateTask),
		}, nil
	}
	// we expect one task
	if len(runTaskOut.Tasks) > 0 {
		log.Printf("started Add to AppStore for ID %s from %s in task %s",
			registrationId,
			sourceUrlValue,
			aws.ToString(runTaskOut.Tasks[0].TaskArn))
	}

	m, err := json.Marshal(models.AppStoreRegistrationResponse{RegistrationId: registrationId})
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

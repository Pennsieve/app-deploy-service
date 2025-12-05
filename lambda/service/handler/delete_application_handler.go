package handler

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	"github.com/aws/aws-sdk-go-v2/service/ecs/types"
	"github.com/pennsieve/app-deploy-service/service/models"
	"github.com/pennsieve/app-deploy-service/service/runner"
	"github.com/pennsieve/app-deploy-service/service/store_dynamodb"
	"github.com/pennsieve/pennsieve-go-core/pkg/authorizer"
)

func DeleteApplicationHandler(ctx context.Context, request events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	handlerName := "DeleteApplicationHandler"
	uuid := request.PathParameters["id"]

	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		log.Println(err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       handlerError(handlerName, ErrConfig),
		}, nil
	}

	TaskDefinitionArn := os.Getenv("TASK_DEF_ARN")
	subIdStr := os.Getenv("SUBNET_IDS")
	SubNetIds := strings.Split(subIdStr, ",")
	cluster := os.Getenv("CLUSTER_ARN")
	SecurityGroup := os.Getenv("SECURITY_GROUP")
	envValue := os.Getenv("ENV")
	TaskDefContainerName := os.Getenv("TASK_DEF_CONTAINER_NAME")

	claims := authorizer.ParseClaims(request.RequestContext.Authorizer.Lambda)
	organizationId := claims.OrgClaim.NodeId
	userId := claims.UserClaim.NodeId

	dynamoDBClient := dynamodb.NewFromConfig(cfg)
	applicationsTable := os.Getenv("APPLICATIONS_TABLE")

	applicationIdKey := "APPLICATION_UUID"

	dynamo_store := store_dynamodb.NewApplicationDatabaseStore(dynamoDBClient, applicationsTable)
	statusManager := NewStatusManager(handlerName, dynamo_store, uuid)
	application, err := dynamo_store.GetById(ctx, uuid)
	if err != nil {
		log.Println(err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusNotFound,
			Body:       handlerError(handlerName, ErrNoRecordsFound),
		}, nil
	}
	statusManager.UpdateApplicationStatus(ctx, uuid, "deleting")

	client := ecs.NewFromConfig(cfg)
	log.Println("Initiating new Provisioning Fargate Task.")
	envKey := "ENV"
	organizationIdKey := "ORG_ID"
	organizationIdValue := organizationId
	userIdKey := "USER_ID"
	userIdValue := userId
	actionKey := "ACTION"
	actionValue := "DELETE"
	tableKey := "APPLICATIONS_TABLE"
	tableValue := applicationsTable
	accountIdKey := "ACCOUNT_ID"
	accountIdValue := application.AccountId
	accountTypeKey := "ACCOUNT_TYPE"
	accountTypeValue := application.AccountType
	accountUuidKey := "ACCOUNT_UUID"
	accountUuidValue := application.AccountUuid
	computeNodeUuidKey := "COMPUTE_NODE_UUID"
	computeNodeUuidValue := application.ComputeNodeUuid
	sourceUrlKey := "SOURCE_URL"
	sourceUrlValue := application.SourceUrl
	runOnGPUKey := "RUN_ON_GPU"
	runOnGPUValue := strconv.FormatBool(application.RunOnGPU)

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
							Name:  &applicationIdKey,
							Value: &uuid,
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
							Name:  &tableKey,
							Value: &tableValue,
						},
						{
							Name:  &organizationIdKey,
							Value: &organizationIdValue,
						},
						{
							Name:  &userIdKey,
							Value: &userIdValue,
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
							Name:  &computeNodeUuidKey,
							Value: &computeNodeUuidValue,
						},
						{
							Name:  &sourceUrlKey,
							Value: &sourceUrlValue,
						},
						{
							Name:  &runOnGPUKey,
							Value: &runOnGPUValue,
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
		log.Println(err)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: 500,
			Body:       statusManager.SetErrorStatus(ctx, ErrRunningFargateTask),
		}, nil
	}
	if err := runner.GetRunFailures(runTaskOut); err != nil {
		log.Println(err)
		// assuming here that if there were failures, then no tasks started.
		// seems safe since for now we are only starting one task
		return events.APIGatewayV2HTTPResponse{
			StatusCode: 500,
			Body:       statusManager.SetErrorStatus(ctx, ErrRunningFargateTask),
		}, nil
	}
	// we expect one task
	if len(runTaskOut.Tasks) > 0 {
		log.Printf("started deletion of application %s from %s in task %s",
			application.ApplicationId,
			sourceUrlValue,
			aws.ToString(runTaskOut.Tasks[0].TaskArn))
	}
	m, err := json.Marshal(models.ApplicationResponse{
		Message: "Application deletion initiated",
	})
	if err != nil {
		log.Println(err.Error())
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

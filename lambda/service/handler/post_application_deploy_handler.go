package handler

import (
	"context"
	"encoding/json"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/google/uuid"
	"github.com/pennsieve/app-deploy-service/service/store_dynamodb"
	"github.com/pennsieve/pennsieve-go-core/pkg/models/role"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	"github.com/aws/aws-sdk-go-v2/service/ecs/types"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/pennsieve/app-deploy-service/service/models"
	"github.com/pennsieve/app-deploy-service/service/runner"
	"github.com/pennsieve/pennsieve-go-core/pkg/authorizer"
)

func PostApplicationDeployHandler(ctx context.Context, request events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	handlerName := "PostApplicationDeployHandler"
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

	claims := authorizer.ParseClaims(request.RequestContext.Authorizer.Lambda)
	// Maybe we should check for role.Writer instead here, but I'm not
	// sure if there is a difference for org roles.
	// So just making sure the user is not a guest
	if !authorizer.HasOrgRole(claims, role.Viewer) {
		log.Printf("user not permitted to deploy application with claims: %+v", claims)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusUnauthorized,
			Body:       handlerError(handlerName, ErrNotPermitted),
		}, nil
	}
	organizationId := claims.OrgClaim.NodeId
	userId := claims.UserClaim.NodeId

	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		log.Println(err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       handlerError(handlerName, ErrConfig),
		}, nil
	}

	client := ecs.NewFromConfig(cfg)
	log.Println("Initiating new Provisioning Fargate Task.")
	envKey := "ENV"
	accountIdKey := "ACCOUNT_ID"
	accountIdValue := application.Account.AccountId
	accountTypeKey := "ACCOUNT_TYPE"
	accountTypeValue := application.Account.AccountType
	accountUuidKey := "ACCOUNT_UUID"
	accountUuidValue := application.Account.Uuid
	organizationIdKey := "ORG_ID"
	organizationIdValue := organizationId
	userIdKey := "USER_ID"
	userIdValue := userId
	actionKey := "ACTION"
	actionValue := "DEPLOY"
	tableKey := "APPLICATIONS_TABLE"
	tableValue := os.Getenv("APPLICATIONS_TABLE")
	applicationNameKey := "APPLICATION_NAME"
	applicationDescriptionKey := "APPLICATION_DESCRIPTION"
	nameValue := application.Name
	descriptionValue := application.Description

	computeNodeUuidKey := "COMPUTE_NODE_UUID"
	computeNodeEfsIdKey := "COMPUTE_NODE_EFS_ID"
	computeNodeUuidValue := application.ComputeNode.Uuid
	computeNodeEfsIdValue := application.ComputeNode.EfsId

	sourceTypeKey := "SOURCE_TYPE"
	sourceTypeValue := application.Source.SourceType
	sourceUrlKey := "SOURCE_URL"
	sourceUrlValue := application.Source.Url

	destinationTypeKey := "DESTINATION_TYPE"
	destinationTypeValue := application.Destination.DestinationType
	destinationUrlKey := "DESTINATION_URL"
	destinationUrlValue := application.Destination.Url

	applicationTypeKey := "APPLICATION_TYPE"
	applicationTypeValue := application.ApplicationType

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

	deploymentId := uuid.NewString()
	dynamoDBClient := dynamodb.NewFromConfig(cfg)
	deploymentsStore := store_dynamodb.NewDeploymentsStore(dynamoDBClient, deploymentsTable)
	if err := deploymentsStore.Insert(ctx, store_dynamodb.Deployment{
		Id:              deploymentId,
		ApplicationId:   application.ApplicationId,
		CreatedAt:       time.Now().UTC(),
		WorkspaceNodeId: organizationId,
		UserNodeId:      userId,
	}); err != nil {
		log.Fatalf("error creating deployment record: %v", err)
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
					Name: &TaskDefContainerName,
					Environment: []types.KeyValuePair{
						{
							Name:  &envKey,
							Value: &envValue,
						},
						{
							Name:  &applicationNameKey,
							Value: &nameValue,
						},
						{
							Name:  &applicationDescriptionKey,
							Value: &descriptionValue,
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
							Name:  &sourceTypeKey,
							Value: &sourceTypeValue,
						},
						{
							Name:  &sourceUrlKey,
							Value: &sourceUrlValue,
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
							Name:  &computeNodeUuidKey,
							Value: &computeNodeUuidValue,
						},
						{
							Name:  &computeNodeEfsIdKey,
							Value: &computeNodeEfsIdValue,
						},
						{
							Name:  &applicationTypeKey,
							Value: &applicationTypeValue,
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
							Name:  aws.String(deploymentIdKey),
							Value: aws.String(deploymentId),
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
			Body:       handlerError(handlerName, ErrRunningFargateTask),
		}, nil
	}
	if err := runner.GetRunFailures(runTaskOut); err != nil {
		log.Println(err)
		// assuming here that if there were failures, then no tasks started.
		// seems safe since for now we are only starting one task
		return events.APIGatewayV2HTTPResponse{
			StatusCode: 500,
			Body:       handlerError(handlerName, ErrRunningFargateTask),
		}, nil
	}
	// we expect one task
	if len(runTaskOut.Tasks) > 0 {
		log.Printf("started re-deployment %s of application %s from %s in task %s",
			deploymentId,
			application.ApplicationId,
			sourceUrlValue,
			aws.StringValue(runTaskOut.Tasks[0].TaskArn))
	}

	m, err := json.Marshal(models.DeployApplicationResponse{DeploymentId: deploymentId})
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

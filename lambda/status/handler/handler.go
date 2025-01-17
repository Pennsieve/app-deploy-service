package handler

import (
	"context"
	"fmt"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	"github.com/aws/aws-sdk-go-v2/service/ecs/types"
	"github.com/pennsieve/app-deploy-service/status/clients"
	"github.com/pennsieve/app-deploy-service/status/logging"
	"github.com/pennsieve/app-deploy-service/status/models"
	"log/slog"
	"slices"
)

type DeployTaskStateChangeHandler struct {
	ECSApi            clients.ECSApi
	DynamoDBApi       clients.DynamoDBApi
	ApplicationsTable string
	DeploymentsTable  string
}

func NewDeployTaskStateChangeHandler(ecsApi clients.ECSApi, dynamoDBApi clients.DynamoDBApi, applicationsTable string, deploymentsTable string) *DeployTaskStateChangeHandler {
	return &DeployTaskStateChangeHandler{ECSApi: ecsApi, DynamoDBApi: dynamoDBApi, ApplicationsTable: applicationsTable, DeploymentsTable: deploymentsTable}
}

func (h *DeployTaskStateChangeHandler) Handle(ctx context.Context, event models.TaskStateChangeEvent) error {
	taskArn := event.Detail.TaskArn
	logger := logging.Default.With(slog.String("taskArn", taskArn))
	logger.Info("handling event", slog.Any("event", event))

	// Ideally, the event would already include the tags we put on in the provisioner task, but that
	// is not the case
	taskDesc, err := h.ECSApi.DescribeTasks(ctx, &ecs.DescribeTasksInput{
		Tasks:   []string{taskArn},
		Cluster: aws.String(event.Detail.ClusterArn),
		Include: []types.TaskField{types.TaskFieldTags},
	})
	if err != nil {
		return fmt.Errorf("error getting task description: %w", err)
	}
	taskIndex := slices.IndexFunc(taskDesc.Tasks, func(task types.Task) bool {
		return aws.ToString(task.TaskArn) == taskArn
	})
	if taskIndex < 0 {
		return fmt.Errorf("unable to find description for task %s", taskArn)
	}

	tagIndex := slices.IndexFunc(taskDesc.Tasks[taskIndex].Tags, func(tag types.Tag) bool {
		return aws.ToString(tag.Key) == DeploymentIdTag
	})
	if tagIndex < 0 {
		return fmt.Errorf("task %s missing deployment id", taskArn)
	}

	deploymentId := aws.ToString(taskDesc.Tasks[taskIndex].Tags[tagIndex].Value)
	logger = logger.With(slog.String("deploymentId", deploymentId))

	deployment, err := h.GetDeployment(ctx, deploymentId)
	if err != nil {
		return fmt.Errorf("error getting deployment %s: %w", deploymentId, err)
	}

	if err := h.UpdateDeploymentsTable(ctx, deployment, event); err != nil {
		return err
	}

	if final := IsFinalState(event); final != nil {
		if err := h.UpdateApplicationsTable(ctx, deployment.ApplicationId, final); err != nil {
			return err
		}
	}

	return nil
}

func (h *DeployTaskStateChangeHandler) GetDeployment(ctx context.Context, deploymentId string) (models.Deployment, error) {
	return models.Deployment{}, nil
}

func (h *DeployTaskStateChangeHandler) UpdateApplicationsTable(ctx context.Context, applicationId string, finalState *FinalState) error {
	return nil
}

func (h *DeployTaskStateChangeHandler) UpdateDeploymentsTable(ctx context.Context, existingDeployment models.Deployment, event models.TaskStateChangeEvent) error {
	return nil
}

type FinalState struct {
	Errored bool
}

func IsFinalState(event models.TaskStateChangeEvent) *FinalState {
	if event.Detail.LastStatus != models.StateStopped {
		return nil
	}
	return &FinalState{Errored: event.Detail.Errored()}
}

package handler

import (
	"context"
	"fmt"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	"github.com/aws/aws-sdk-go-v2/service/ecs/types"
	"github.com/pennsieve/app-deploy-service/status/dydbutils"
	"github.com/pennsieve/app-deploy-service/status/external"
	"github.com/pennsieve/app-deploy-service/status/logging"
	"github.com/pennsieve/app-deploy-service/status/models"
	"log/slog"
	"slices"
)

type DeployTaskStateChangeHandler struct {
	ECSApi            external.ECSApi
	DynamoDBApi       external.DynamoDBApi
	ApplicationsTable string
	DeploymentsTable  string
}

func NewDeployTaskStateChangeHandler(ecsApi external.ECSApi, dynamoDBApi external.DynamoDBApi, applicationsTable string, deploymentsTable string) *DeployTaskStateChangeHandler {
	return &DeployTaskStateChangeHandler{ECSApi: ecsApi, DynamoDBApi: dynamoDBApi, ApplicationsTable: applicationsTable, DeploymentsTable: deploymentsTable}
}

func (h *DeployTaskStateChangeHandler) Handle(ctx context.Context, event models.TaskStateChangeEvent) error {
	taskArn := event.Detail.TaskArn
	logger := logging.Default.With(slog.String("taskArn", taskArn))
	logger.Info("handling event", slog.Any("event", event))

	ids, err := h.GetIdsFromTags(ctx, taskArn, event.Detail.ClusterArn)
	if err != nil {
		return fmt.Errorf("error getting ids from task tags: %w", err)
	}

	deploymentId := ids.DeploymentId
	applicationId := ids.ApplicationId
	logger = logger.With(
		slog.String("deploymentId", deploymentId),
		slog.String("applicationId", applicationId))

	deployment, err := h.GetDeployment(ctx, deploymentId)
	if err != nil {
		return fmt.Errorf("error getting deployment %s: %w", deploymentId, err)
	}

	if deployment == nil {
		deployment, err = h.StoreNewDeployment(ctx, deploymentId, applicationId, event)
		if err != nil {
			return fmt.Errorf("error storing new deployment %s: %w", deploymentId, err)
		}
	} else if err := h.UpdateDeploymentsTable(ctx, deploymentId, applicationId, event, deployment); err != nil {
		return err
	}

	if final := IsFinalState(event); final != nil {
		if err := h.UpdateApplicationsTable(ctx, applicationId, final); err != nil {
			return err
		}
	}

	return nil
}

type DeploymentApplicationIds struct {
	DeploymentId  string
	ApplicationId string
}

func (i DeploymentApplicationIds) checkIds() error {
	if len(i.DeploymentId) == 0 {
		return fmt.Errorf("missing deployment id")
	}
	if len(i.ApplicationId) == 0 {
		return fmt.Errorf("missing application id")
	}
	return nil
}

// GetIdsFromTags looks up the deployment and application ids for the given task by looking at its tags.
// Ideally, the event we are handling would already include the tags we put on in the provisioner task, but that
// is not the case
func (h *DeployTaskStateChangeHandler) GetIdsFromTags(ctx context.Context, taskArn string, clusterArn string) (DeploymentApplicationIds, error) {
	ids := DeploymentApplicationIds{}
	taskDesc, err := h.ECSApi.DescribeTasks(ctx, &ecs.DescribeTasksInput{
		Tasks:   []string{taskArn},
		Cluster: aws.String(clusterArn),
		Include: []types.TaskField{types.TaskFieldTags},
	})
	if err != nil {
		return ids, fmt.Errorf("error getting task description for task %s: %w", taskArn, err)
	}
	taskIndex := slices.IndexFunc(taskDesc.Tasks, func(task types.Task) bool {
		return aws.ToString(task.TaskArn) == taskArn
	})
	if taskIndex < 0 {
		return ids, fmt.Errorf("unable to find description for task %s", taskArn)
	}

	for _, tag := range taskDesc.Tasks[taskIndex].Tags {
		key := aws.ToString(tag.Key)
		if key == DeploymentIdTag {
			ids.DeploymentId = aws.ToString(tag.Value)
		} else if key == ApplicationIdTag {
			ids.ApplicationId = aws.ToString(tag.Value)
		}
	}
	if err := ids.checkIds(); err != nil {
		return ids, fmt.Errorf("task %s missing id tags: %w", taskArn, err)
	}

	return ids, nil
}

func (h *DeployTaskStateChangeHandler) GetDeployment(ctx context.Context, deploymentId string) (*models.Deployment, error) {
	key := models.DeploymentKey(deploymentId)
	getItemIn := &dynamodb.GetItemInput{
		Key:                      key,
		TableName:                aws.String(h.DeploymentsTable),
		ConsistentRead:           aws.Bool(true),
		ExpressionAttributeNames: nil,
	}
	getItemOut, err := h.DynamoDBApi.GetItem(ctx, getItemIn)
	if err != nil {
		return nil, fmt.Errorf("error getting deployment %s: %w", deploymentId, err)
	}
	deployment, err := dydbutils.FromItem[models.Deployment](getItemOut.Item)
	if err != nil {
		return nil, fmt.Errorf("error unmarshalling deployment %s: %w", deploymentId, err)
	}
	return deployment, nil
}

func (h *DeployTaskStateChangeHandler) UpdateApplicationsTable(ctx context.Context, applicationId string, finalState *FinalState) error {
	return nil
}

func (h *DeployTaskStateChangeHandler) StoreNewDeployment(ctx context.Context, deploymentId string, applicationId string, event models.TaskStateChangeEvent) (*models.Deployment, error) {
	return nil, nil
}

func (h *DeployTaskStateChangeHandler) UpdateDeploymentsTable(ctx context.Context, deploymentId string, applicationId string, event models.TaskStateChangeEvent, existingDeployment *models.Deployment) error {
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

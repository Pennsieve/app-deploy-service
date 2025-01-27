package handler

import (
	"context"
	"fmt"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	"github.com/aws/aws-sdk-go-v2/service/ecs/types"
	"slices"
)

type DeploymentApplicationIds struct {
	DeploymentId  string
	ApplicationId string
}

func (i DeploymentApplicationIds) CheckIds() error {
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
	if err := ids.CheckIds(); err != nil {
		return ids, fmt.Errorf("task %s missing id tags: %w", taskArn, err)
	}

	return ids, nil
}

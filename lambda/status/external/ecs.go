package external

import (
	"context"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
)

type ECSApi interface {
	DescribeTasks(ctx context.Context, params *ecs.DescribeTasksInput, optFns ...func(*ecs.Options)) (*ecs.DescribeTasksOutput, error)
}

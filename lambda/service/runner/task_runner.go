package runner

import (
	"context"
	"errors"
	"fmt"
	"github.com/aws/aws-sdk-go/aws"

	"github.com/aws/aws-sdk-go-v2/service/ecs"
)

type ECSTaskRunner struct {
	Client *ecs.Client
	Input  *ecs.RunTaskInput
}

func NewECSTaskRunner(client *ecs.Client, input *ecs.RunTaskInput) ResultRunner[*ecs.RunTaskOutput] {
	return &ECSTaskRunner{client, input}
}

func (r *ECSTaskRunner) Run(ctx context.Context) (*ecs.RunTaskOutput, error) {
	return r.Client.RunTask(ctx, r.Input)
}

// GetRunFailures returns nil if runTaskOut contains no types.Failure,
// otherwise it combines all the types.Failure into a single error
func GetRunFailures(runTaskOut *ecs.RunTaskOutput) error {
	var errs []error
	for _, failure := range runTaskOut.Failures {
		errs = append(errs, fmt.Errorf("run task failure: arn: %s, reason: %s, detail: %s",
			aws.StringValue(failure.Arn),
			aws.StringValue(failure.Reason),
			aws.StringValue(failure.Detail)))
	}
	return errors.Join(errs...)
}

package provisioner

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
)

type Provisioner interface {
	Run(context.Context) error
	AssumeRole(context.Context) (aws.Credentials, error)
	CreatePolicy(context.Context) error
}

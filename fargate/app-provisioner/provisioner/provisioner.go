package provisioner

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
)

type Provisioner interface {
	Create(ctx context.Context) error
	Delete(ctx context.Context) error
	AssumeRole(context.Context) (aws.Credentials, error)
	CreatePolicy(context.Context) error
	GetPolicy(context.Context) (*string, error)
}

package provisioner

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
)

type Provisioner interface {
	Create(ctx context.Context) error
	Delete(ctx context.Context) error
	AssumeRole(context.Context) (aws.Credentials, error)
	CreatePublicRepository(ctx context.Context) error
	GetProvisionerCreds(context.Context) (aws.Credentials, error)
}

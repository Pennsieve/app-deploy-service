package external

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/service/ecr"
)

// ECRApi reads the digest of an image an App Store build pushed.
type ECRApi interface {
	DescribeImages(ctx context.Context, params *ecr.DescribeImagesInput, optFns ...func(*ecr.Options)) (*ecr.DescribeImagesOutput, error)
}

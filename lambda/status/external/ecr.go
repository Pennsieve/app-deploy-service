package external

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/service/ecr"
)

// ECRApi reads the image an App Store build pushed: its digest, and its
// manifest and configuration, to decide whether it can run on Lambda; and
// deletes a promoted build's staging tag.
type ECRApi interface {
	DescribeImages(ctx context.Context, params *ecr.DescribeImagesInput, optFns ...func(*ecr.Options)) (*ecr.DescribeImagesOutput, error)
	BatchDeleteImage(ctx context.Context, params *ecr.BatchDeleteImageInput, optFns ...func(*ecr.Options)) (*ecr.BatchDeleteImageOutput, error)
	BatchGetImage(ctx context.Context, params *ecr.BatchGetImageInput, optFns ...func(*ecr.Options)) (*ecr.BatchGetImageOutput, error)
	GetDownloadUrlForLayer(ctx context.Context, params *ecr.GetDownloadUrlForLayerInput, optFns ...func(*ecr.Options)) (*ecr.GetDownloadUrlForLayerOutput, error)
}

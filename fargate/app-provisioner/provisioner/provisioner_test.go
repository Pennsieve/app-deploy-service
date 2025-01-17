package provisioner_test

import (
	"context"
	"github.com/aws/aws-sdk-go-v2/aws"
	"testing"

	awsProvisioner "github.com/pennsieve/app-deploy-service/app-provisioner/provisioner/aws"
	"github.com/stretchr/testify/assert"
)

func TestAWSProvisioner(t *testing.T) {
	provisioner := awsProvisioner.NewAWSProvisioner(aws.Config{}, "someAccountId", "UNKNOWN_ACTION", "dev", "someUrl", "SomeEfsId", "someSlug")
	err := provisioner.Run(context.Background())
	assert.Equal(t, "action not supported: UNKNOWN_ACTION", err.Error())
}

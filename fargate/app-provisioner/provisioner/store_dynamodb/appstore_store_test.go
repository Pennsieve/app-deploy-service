package store_dynamodb

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVariantUpdateInput(t *testing.T) {
	in, err := variantUpdateInput("versions", "v-1", "gpu", VariantUpdate{DestinationUrl: "repo:1-v1--gpu", Status: "deploying"})
	require.NoError(t, err)
	assert.Equal(t, "SET #vs.#id.#destinationUrl = :destinationUrl, #vs.#id.#status = :status", aws.ToString(in.UpdateExpression))
	assert.Equal(t, "attribute_exists(#vs.#id)", aws.ToString(in.ConditionExpression))
	assert.Equal(t, "gpu", in.ExpressionAttributeNames["#id"])
	assert.Equal(t, "repo:1-v1--gpu", in.ExpressionAttributeValues[":destinationUrl"].(*types.AttributeValueMemberS).Value)
	assert.Equal(t, "v-1", in.Key["uuid"].(*types.AttributeValueMemberS).Value)

	in, err = variantUpdateInput("versions", "v-1", "gpu", VariantUpdate{Status: "error", FailureReason: "bad arg"})
	require.NoError(t, err)
	assert.Equal(t, "SET #vs.#id.#status = :status, #vs.#id.#failureReason = :failureReason", aws.ToString(in.UpdateExpression))

	in, err = variantUpdateInput("versions", "v-1", "gpu", VariantUpdate{})
	require.NoError(t, err)
	assert.Nil(t, in)
}

// What the service lambda stores reads back as the builds.
func TestVersionBuildsUnmarshal(t *testing.T) {
	item := map[string]any{
		"uuid":           "v-1",
		"defaultVariant": "cpu",
		"variantOrder":   []string{"cpu", "gpu"},
		"variants": map[string]any{
			"cpu": map[string]any{"name": "CPU", "deploymentId": "d-1", "build": map[string]any{"args": map[string]string{"TORCH_FLAVOUR": "cpu"}}},
			"gpu": map[string]any{"name": "GPU", "deploymentId": "d-2", "status": "registering", "buildStorageGiB": 100,
				"build": map[string]any{"args": map[string]string{"TORCH_FLAVOUR": "cuda"}, "target": "gpu", "dockerfile": "docker/Dockerfile"}},
		},
	}
	av, err := attributevalue.MarshalMap(item)
	require.NoError(t, err)
	var b VersionBuilds
	require.NoError(t, attributevalue.UnmarshalMap(av, &b))
	assert.Equal(t, "cpu", b.DefaultVariant)
	assert.Equal(t, []string{"cpu", "gpu"}, b.VariantOrder)
	assert.Equal(t, VariantBuild{DeploymentId: "d-2", BuildStorageGiB: 100,
		Build: &BuildSettings{Args: map[string]string{"TORCH_FLAVOUR": "cuda"}, Target: "gpu", Dockerfile: "docker/Dockerfile"}}, b.Variants["gpu"])
	assert.Nil(t, b.Build)
}

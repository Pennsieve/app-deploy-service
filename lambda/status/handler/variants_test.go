package handler

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	ecstypes "github.com/aws/aws-sdk-go-v2/service/ecs/types"
	"github.com/pennsieve/app-deploy-service/status/events"
	"github.com/pennsieve/app-deploy-service/status/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var placeholder = regexp.MustCompile(`[#:]\w+`)

// readable spells out an expression with its names and string values.
func readable(expr string, in *dynamodb.UpdateItemInput) string {
	return placeholder.ReplaceAllStringFunc(expr, func(p string) string {
		if strings.HasPrefix(p, "#") {
			return in.ExpressionAttributeNames[p]
		}
		if s, ok := in.ExpressionAttributeValues[p].(*types.AttributeValueMemberS); ok {
			return `"` + s.Value + `"`
		}
		return p
	})
}

// variantBuildEvent is a CodeBuild event of a variant's build.
func variantBuildEvent(status, variant string, isDefault bool) []byte {
	def := "false"
	if isDefault {
		def = "true"
	}
	extra := `{"name": "VARIANT", "type": "PLAINTEXT", "value": "` + variant + `"},
			{"name": "VARIANT_DEFAULT", "type": "PLAINTEXT", "value": "` + def + `"},
			{"name": "GIT_TOKEN"`
	return []byte(strings.Replace(string(buildEvent(status, true)), `{"name": "GIT_TOKEN"`, extra, 1))
}

func TestHandleBuild_OtherVariantFailed(t *testing.T) {
	h, db, _, _, _ := newBuildTestHandler()
	publisher := &recordingPublisher{}
	h.WithRealtime(publisher)
	require.NoError(t, h.HandleEvent(context.Background(), variantBuildEvent(models.BuildFailed, "gpu", false)))

	versions := db.tableUpdates(testVersionsTable)
	require.Len(t, versions, 1, "only the variant: the version's status is untouched")
	u := versions[0]
	assert.Equal(t, models.ApplicationKey("version-1"), u.Key)
	expr := readable(aws.ToString(u.UpdateExpression), u)
	assert.Contains(t, expr, `variants.gpu.status = "error"`)
	assert.Regexp(t, `variants\.gpu\.failureReason = "image build FAILED in phase BUILD`, expr)
	assert.Equal(t, "attribute_exists (variants.gpu)", readable(aws.ToString(u.ConditionExpression), u))

	require.Len(t, publisher.names, 1)
	assert.Equal(t, events.VariantStatusEventName, publisher.names[0])
	ev := publisher.data[0].(events.ApplicationStatusEvent)
	assert.Equal(t, "gpu", ev.Variant)
	assert.True(t, ev.IsErrorStatus)
}

func TestHandleBuild_DefaultVariantSucceeded(t *testing.T) {
	h, db, _, _, _ := newBuildTestHandler()
	publisher := &recordingPublisher{}
	h.WithRealtime(publisher)
	require.NoError(t, h.HandleEvent(context.Background(), variantBuildEvent(models.BuildSucceeded, "cpu", true)))

	versions := db.tableUpdates(testVersionsTable)
	require.Len(t, versions, 2)
	assert.Equal(t, "deployed", stringValue(t, setValues(versions[0])[models.ApplicationStatusField]), "the version follows its default variant")
	variant := readable(aws.ToString(versions[1].UpdateExpression), versions[1])
	assert.Contains(t, variant, `variants.cpu.status = "deployed"`)
	assert.Contains(t, variant, `REMOVE variants.cpu.failureReason`)

	require.Len(t, publisher.names, 1)
	assert.Equal(t, events.ApplicationStatusEventName, publisher.names[0], "the version's own event, naming the variant")
	assert.Equal(t, "cpu", publisher.data[0].(events.ApplicationStatusEvent).Variant)
}

func TestHandleBuild_DefaultVariantFailed(t *testing.T) {
	h, db, _, _, _ := newBuildTestHandler()
	require.NoError(t, h.HandleEvent(context.Background(), variantBuildEvent(models.BuildFailed, "cpu", true)))

	versions := db.tableUpdates(testVersionsTable)
	require.Len(t, versions, 2)
	assert.Equal(t, "error", stringValue(t, setValues(versions[0])[models.ApplicationStatusField]))
	assert.Contains(t, readable(aws.ToString(versions[1].UpdateExpression), versions[1]), `variants.cpu.status = "error"`)
}

// A variant's build in progress touches only its deployment.
func TestHandleBuild_VariantInProgress(t *testing.T) {
	h, db, _, _, _ := newBuildTestHandler()
	require.NoError(t, h.HandleEvent(context.Background(), variantBuildEvent(models.BuildInProgress, "gpu", false)))
	assert.Len(t, db.tableUpdates(testDeploymentsTable), 1)
	assert.Empty(t, db.tableUpdates(testVersionsTable))
}

// Regression: a build without a variant records exactly what it did before
// variants: one version update, of registrationStatus, and the version's
// event without a variant.
func TestHandleBuild_NoVariantUnchanged(t *testing.T) {
	h, db, _, _, _ := newBuildTestHandler()
	publisher := &recordingPublisher{}
	h.WithRealtime(publisher)
	require.NoError(t, h.HandleEvent(context.Background(), buildEvent(models.BuildFailed, true)))

	versions := db.tableUpdates(testVersionsTable)
	require.Len(t, versions, 1)
	assert.Equal(t, `SET registrationStatus = "error"`, strings.TrimSpace(readable(aws.ToString(versions[0].UpdateExpression), versions[0])))
	require.Len(t, publisher.names, 1)
	assert.Equal(t, events.ApplicationStatusEventName, publisher.names[0])
	assert.Empty(t, publisher.data[0].(events.ApplicationStatusEvent).Variant)
}

type variantECS struct{ variant, isDefault string }

func (f variantECS) DescribeTasks(_ context.Context, in *ecs.DescribeTasksInput, _ ...func(*ecs.Options)) (*ecs.DescribeTasksOutput, error) {
	return &ecs.DescribeTasksOutput{Tasks: []ecstypes.Task{{
		TaskArn: aws.String(in.Tasks[0]),
		Tags: []ecstypes.Tag{
			{Key: aws.String(DeploymentIdTag), Value: aws.String("deployment-2")},
			{Key: aws.String(ApplicationIdTag), Value: aws.String("version-1")},
			{Key: aws.String(ApplicationsTableTag), Value: aws.String(testVersionsTable)},
			{Key: aws.String(VariantTag), Value: aws.String(f.variant)},
			{Key: aws.String(VariantDefaultTag), Value: aws.String(f.isDefault)},
		},
	}}}, nil
}

// The kaniko task of a non-default variant: its variant fails, with the
// exit code, and the version is left alone.
func TestHandleEvent_ECSVariantTask(t *testing.T) {
	db := &recordingDynamoDB{}
	h := NewDeployTaskStateChangeHandler(variantECS{"gpu", "false"}, db, testApplicationsTable, testDeploymentsTable)
	event := []byte(`{
		"source": "aws.ecs",
		"detail-type": "ECS Task State Change",
		"detail": {
			"taskArn": "arn:aws:ecs:us-east-1:123456789012:task/c/abc",
			"clusterArn": "arn:aws:ecs:us-east-1:123456789012:cluster/c",
			"lastStatus": "STOPPED",
			"desiredStatus": "STOPPED",
			"stoppedReason": "Essential container in task exited",
			"version": 4,
			"containers": [{"exitCode": 1}]
		}
	}`)
	require.NoError(t, h.HandleEvent(context.Background(), event))
	assert.Equal(t, models.DeploymentKeyItem("version-1", "deployment-2"), db.tableUpdates(testDeploymentsTable)[0].Key)
	versions := db.tableUpdates(testVersionsTable)
	require.Len(t, versions, 1)
	expr := readable(aws.ToString(versions[0].UpdateExpression), versions[0])
	assert.Contains(t, expr, `variants.gpu.status = "error"`)
	assert.Contains(t, expr, `variants.gpu.failureReason = "image build exited with code 1: Essential container in task exited"`)
	assert.Empty(t, db.tableUpdates(testApplicationsTable))
}

func TestVariantFailureReason(t *testing.T) {
	ok := models.TaskStateChangeEvent{Detail: models.Detail{Containers: []models.Container{{ExitCode: 0}}}}
	assert.Empty(t, VariantFailureReason(ok))
	failed := models.TaskStateChangeEvent{Detail: models.Detail{Containers: []models.Container{{ExitCode: 137}}}}
	assert.Equal(t, "image build exited with code 137", VariantFailureReason(failed))
	build := models.TaskStateChangeEvent{Detail: models.Detail{BuildFailed: true, StoppedReason: "image build FAILED"}}
	assert.Equal(t, "image build FAILED", VariantFailureReason(build))

	in, err := VariantStatusUpdate("v-1", "gpu", &FinalState{Errored: true}, "", testVersionsTable)
	require.NoError(t, err)
	assert.Contains(t, readable(aws.ToString(in.UpdateExpression), in), `variants.gpu.failureReason = "image build failed"`)
}

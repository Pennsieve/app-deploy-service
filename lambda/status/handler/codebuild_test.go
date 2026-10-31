package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	logtypes "github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs/types"
	"github.com/aws/aws-sdk-go-v2/service/codebuild"
	cbtypes "github.com/aws/aws-sdk-go-v2/service/codebuild/types"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	ecstypes "github.com/aws/aws-sdk-go-v2/service/ecs/types"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/pennsieve/app-deploy-service/status/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testDeploymentsTable  = "dev-deployments-table"
	testApplicationsTable = "dev-applications-table"
	testVersionsTable     = "dev-appstore-versions-table"
	testBuildId           = "arn:aws:codebuild:us-east-1:123456789012:build/dev-app-deploy-service-image-builder-use1:8745a7a9"
	testTokenParameter    = "/dev/app-deploy-service/image-builds/deployment-1/git-token"
)

// buildEvent is a CodeBuild "Build State Change" event, shaped like the
// example in the CodeBuild user guide, for the image-builder project.
func buildEvent(status string, withVariables bool) []byte {
	variables := `[]`
	if withVariables {
		variables = `[
			{"name": "BUILDKIT_VERSION", "type": "PLAINTEXT", "value": "v0.34.1"},
			{"name": "CONTEXT", "type": "PLAINTEXT", "value": "https://github.com/org/app.git#refs/tags/v1.0.0"},
			{"name": "DEPLOYMENT_ID", "type": "PLAINTEXT", "value": "deployment-1"},
			{"name": "APPLICATION_ID", "type": "PLAINTEXT", "value": "version-1"},
			{"name": "APPLICATIONS_TABLE", "type": "PLAINTEXT", "value": "` + testVersionsTable + `"},
			{"name": "GIT_TOKEN", "type": "PARAMETER_STORE", "value": "` + testTokenParameter + `"}
		]`
	}
	phases := `[
		{"phase-type": "SUBMITTED", "phase-status": "SUCCEEDED", "phase-context": []},
		{"phase-type": "INSTALL", "phase-status": "SUCCEEDED", "phase-context": [": "]}
	]`
	if status != models.BuildSucceeded && status != models.BuildInProgress {
		phases = `[
			{"phase-type": "SUBMITTED", "phase-status": "SUCCEEDED", "phase-context": []},
			{"phase-type": "INSTALL", "phase-status": "SUCCEEDED", "phase-context": [": "]},
			{"phase-type": "BUILD", "phase-status": "` + status + `", "phase-context": ["COMMAND_EXECUTION_ERROR: Error while executing command: sh /tmp/build.sh. Reason: exit status 1"]}
		]`
	}
	return []byte(fmt.Sprintf(`{
		"version": "0",
		"id": "c030038d-8c4d-6141-9545-00ff7b7153EX",
		"detail-type": "CodeBuild Build State Change",
		"source": "aws.codebuild",
		"account": "123456789012",
		"time": "2026-10-29T16:20:42Z",
		"region": "us-east-1",
		"resources": ["%[2]s"],
		"detail": {
			"build-status": "%[1]s",
			"project-name": "dev-app-deploy-service-image-builder-use1",
			"build-id": "%[2]s",
			"additional-information": {
				"build-complete": %[3]t,
				"initiator": "dev-app-deploy-service-fargate-task-role-use1/abc",
				"build-start-time": "Oct 29, 2026 4:12:29 PM",
				"source": {"type": "NO_SOURCE"},
				"logs": {
					"group-name": "/aws/codebuild/dev-app-deploy-service-image-builder-use1",
					"stream-name": "8745a7a9",
					"deep-link": "https://console.aws.amazon.com/cloudwatch/home?region=us-east-1#logEvent:group=x;stream=8745a7a9"
				},
				"phases": %[4]s,
				"environment": {
					"image": "aws/codebuild/amazonlinux-x86_64-standard:5.0",
					"privileged-mode": true,
					"compute-type": "BUILD_GENERAL1_LARGE",
					"type": "LINUX_CONTAINER",
					"environment-variables": %[5]s
				},
				"timeout-in-minutes": 60
			},
			"current-phase": "COMPLETED",
			"current-phase-context": "[]",
			"version": "1"
		}
	}`, status, testBuildId, status != models.BuildInProgress, phases, variables))
}

type recordingDynamoDB struct {
	updates []*dynamodb.UpdateItemInput
	// items are what GetItem answers, by table; none: an empty answer
	items map[string]map[string]types.AttributeValue
	gets  []*dynamodb.GetItemInput
}

func (r *recordingDynamoDB) GetItem(_ context.Context, in *dynamodb.GetItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error) {
	r.gets = append(r.gets, in)
	return &dynamodb.GetItemOutput{Item: r.items[aws.ToString(in.TableName)]}, nil
}

func (r *recordingDynamoDB) UpdateItem(_ context.Context, in *dynamodb.UpdateItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.UpdateItemOutput, error) {
	r.updates = append(r.updates, in)
	return &dynamodb.UpdateItemOutput{}, nil
}

func (r *recordingDynamoDB) tableUpdates(table string) []*dynamodb.UpdateItemInput {
	var out []*dynamodb.UpdateItemInput
	for _, u := range r.updates {
		if aws.ToString(u.TableName) == table {
			out = append(out, u)
		}
	}
	return out
}

var setClause = regexp.MustCompile(`(#\w+) = (:\w+)`)

// setValues maps each attribute an update SETs to its value.
func setValues(in *dynamodb.UpdateItemInput) map[string]types.AttributeValue {
	values := map[string]types.AttributeValue{}
	for _, m := range setClause.FindAllStringSubmatch(aws.ToString(in.UpdateExpression), -1) {
		values[in.ExpressionAttributeNames[m[1]]] = in.ExpressionAttributeValues[m[2]]
	}
	return values
}

func stringValue(t *testing.T, v types.AttributeValue) string {
	s, ok := v.(*types.AttributeValueMemberS)
	require.True(t, ok, "want a string, got %T", v)
	return s.Value
}

type fakeLogs struct{ messages []string }

func (f *fakeLogs) GetLogEvents(_ context.Context, _ *cloudwatchlogs.GetLogEventsInput, _ ...func(*cloudwatchlogs.Options)) (*cloudwatchlogs.GetLogEventsOutput, error) {
	out := &cloudwatchlogs.GetLogEventsOutput{}
	for _, m := range f.messages {
		out.Events = append(out.Events, logtypes.OutputLogEvent{Message: aws.String(m)})
	}
	return out, nil
}

type fakeSSM struct{ deleted []string }

func (f *fakeSSM) DeleteParameter(_ context.Context, in *ssm.DeleteParameterInput, _ ...func(*ssm.Options)) (*ssm.DeleteParameterOutput, error) {
	f.deleted = append(f.deleted, aws.ToString(in.Name))
	return &ssm.DeleteParameterOutput{}, nil
}

type fakeCodeBuild struct{ calls int }

func (f *fakeCodeBuild) BatchGetBuilds(_ context.Context, in *codebuild.BatchGetBuildsInput, _ ...func(*codebuild.Options)) (*codebuild.BatchGetBuildsOutput, error) {
	f.calls++
	return &codebuild.BatchGetBuildsOutput{Builds: []cbtypes.Build{{
		Arn: aws.String(in.Ids[0]),
		Environment: &cbtypes.ProjectEnvironment{EnvironmentVariables: []cbtypes.EnvironmentVariable{
			{Name: aws.String("DEPLOYMENT_ID"), Value: aws.String("deployment-1"), Type: cbtypes.EnvironmentVariableTypePlaintext},
			{Name: aws.String("APPLICATION_ID"), Value: aws.String("version-1"), Type: cbtypes.EnvironmentVariableTypePlaintext},
			{Name: aws.String("APPLICATIONS_TABLE"), Value: aws.String(testVersionsTable), Type: cbtypes.EnvironmentVariableTypePlaintext},
			{Name: aws.String("GIT_TOKEN"), Value: aws.String(testTokenParameter), Type: cbtypes.EnvironmentVariableTypeParameterStore},
		}},
	}}}, nil
}

func newBuildTestHandler() (*DeployTaskStateChangeHandler, *recordingDynamoDB, *fakeLogs, *fakeSSM, *fakeCodeBuild) {
	db, logs, params, cb := &recordingDynamoDB{}, &fakeLogs{}, &fakeSSM{}, &fakeCodeBuild{}
	h := NewDeployTaskStateChangeHandler(nil, db, testApplicationsTable, testDeploymentsTable).WithCodeBuild(cb, logs, params)
	return h, db, logs, params, cb
}

func TestHandleEvent_BuildSucceeded(t *testing.T) {
	h, db, _, params, _ := newBuildTestHandler()
	require.NoError(t, h.HandleEvent(context.Background(), buildEvent(models.BuildSucceeded, true)))

	deployments := db.tableUpdates(testDeploymentsTable)
	require.Len(t, deployments, 1)
	assert.Equal(t, models.DeploymentKeyItem("version-1", "deployment-1"), deployments[0].Key)
	values := setValues(deployments[0])
	assert.Equal(t, models.StateStopped, stringValue(t, values[models.DeploymentLastStatusField]))
	assert.Equal(t, testBuildId, stringValue(t, values[models.DeploymentTaskArnField]))
	assert.Equal(t, models.BuildSucceeded, stringValue(t, values[models.DeploymentStopCodeField]))
	assert.Equal(t, &types.AttributeValueMemberBOOL{Value: false}, values[models.DeploymentErroredField])
	assert.Equal(t, &types.AttributeValueMemberN{Value: "2"}, values[models.DeploymentVersionField])
	_, hasReason := values[models.DeploymentStoppedReasonField]
	assert.False(t, hasReason)

	// the version (APPLICATIONS_TABLE from the build), not the default table
	versions := db.tableUpdates(testVersionsTable)
	require.Len(t, versions, 1)
	assert.Equal(t, models.ApplicationKey("version-1"), versions[0].Key)
	assert.Equal(t, "deployed", stringValue(t, setValues(versions[0])[models.ApplicationStatusField]))
	assert.Empty(t, db.tableUpdates(testApplicationsTable))

	assert.Equal(t, []string{testTokenParameter}, params.deleted, "the token parameter goes when the build ends")
}

func TestHandleEvent_BuildFailedStates(t *testing.T) {
	for _, status := range []string{models.BuildFailed, models.BuildFault, models.BuildStopped, models.BuildTimedOut} {
		t.Run(status, func(t *testing.T) {
			h, db, logs, params, _ := newBuildTestHandler()
			logs.messages = []string{
				"[Container] 2026/10/29 16:13:01 Running command sh /tmp/build.sh\n",
				"#9 [6/9] RUN python3 - <<'PY'\n",
				"#9 ERROR: process \"/bin/sh -c python3 -\" did not complete successfully: exit code: 1\n",
				"error: failed to solve: exit code: 1\n",
				"[Container] 2026/10/29 16:20:40 Command did not exit successfully sh /tmp/build.sh exit status 1\n",
				"[Container] 2026/10/29 16:20:40 Phase complete: BUILD State: FAILED\n",
			}
			require.NoError(t, h.HandleEvent(context.Background(), buildEvent(status, true)))

			deployments := db.tableUpdates(testDeploymentsTable)
			require.Len(t, deployments, 1)
			values := setValues(deployments[0])
			assert.Equal(t, models.StateStopped, stringValue(t, values[models.DeploymentLastStatusField]))
			assert.Equal(t, status, stringValue(t, values[models.DeploymentStopCodeField]))
			assert.Equal(t, &types.AttributeValueMemberBOOL{Value: true}, values[models.DeploymentErroredField])

			reason := stringValue(t, values[models.DeploymentStoppedReasonField])
			assert.True(t, strings.HasPrefix(reason, "image build "+status+" in phase BUILD: COMMAND_EXECUTION_ERROR"), reason)
			assert.Contains(t, reason, "error: failed to solve: exit code: 1")
			assert.NotContains(t, reason, "[Container]", "CodeBuild's own lines are left out")
			assert.True(t, strings.HasSuffix(reason, "\nlog: https://console.aws.amazon.com/cloudwatch/home?region=us-east-1#logEvent:group=x;stream=8745a7a9"), reason)

			versions := db.tableUpdates(testVersionsTable)
			require.Len(t, versions, 1)
			assert.Equal(t, "error", stringValue(t, setValues(versions[0])[models.ApplicationStatusField]))
			assert.Equal(t, []string{testTokenParameter}, params.deleted)
		})
	}
}

func TestHandleEvent_BuildInProgress(t *testing.T) {
	h, db, _, params, _ := newBuildTestHandler()
	require.NoError(t, h.HandleEvent(context.Background(), buildEvent(models.BuildInProgress, true)))

	require.Len(t, db.updates, 1, "only the deployment; the version stays deploying")
	values := setValues(db.updates[0])
	assert.Equal(t, "RUNNING", stringValue(t, values[models.DeploymentLastStatusField]))
	assert.Equal(t, &types.AttributeValueMemberN{Value: "1"}, values[models.DeploymentVersionField])
	_, hasErrored := values[models.DeploymentErroredField]
	assert.False(t, hasErrored)
	assert.Empty(t, params.deleted, "the build still needs its token")
}

func TestHandleEvent_BuildIdsFromTheBuildWhenNotInTheEvent(t *testing.T) {
	h, db, _, params, cb := newBuildTestHandler()
	require.NoError(t, h.HandleEvent(context.Background(), buildEvent(models.BuildSucceeded, false)))
	assert.Equal(t, 1, cb.calls)
	assert.Len(t, db.tableUpdates(testVersionsTable), 1)
	assert.Equal(t, []string{testTokenParameter}, params.deleted)
}

func TestHandleEvent_UnknownBuildStatusIsIgnored(t *testing.T) {
	h, db, _, _, _ := newBuildTestHandler()
	require.NoError(t, h.HandleEvent(context.Background(), buildEvent("QUEUED", true)))
	assert.Empty(t, db.updates)
}

func TestBuildFailureReasonIsBounded(t *testing.T) {
	h, _, logs, _, _ := newBuildTestHandler()
	for i := 0; i < buildLogTailLines; i++ {
		logs.messages = append(logs.messages, strings.Repeat("x", 400)+"\n")
	}
	var event models.BuildStateChangeEvent
	require.NoError(t, json.Unmarshal(buildEvent(models.BuildFailed, true), &event))
	reason := h.buildFailureReason(context.Background(), event)
	assert.LessOrEqual(t, len(reason), maxFailureReasonSize)
	assert.True(t, strings.HasPrefix(reason, "image build FAILED in phase BUILD"))
	assert.Contains(t, reason, "\nlog: https://")
}

type fakeECS struct{}

func (fakeECS) DescribeTasks(_ context.Context, in *ecs.DescribeTasksInput, _ ...func(*ecs.Options)) (*ecs.DescribeTasksOutput, error) {
	return &ecs.DescribeTasksOutput{Tasks: []ecstypes.Task{{
		TaskArn: aws.String(in.Tasks[0]),
		Tags: []ecstypes.Tag{
			{Key: aws.String(DeploymentIdTag), Value: aws.String("deployment-1")},
			{Key: aws.String(ApplicationIdTag), Value: aws.String("version-1")},
		},
	}}}, nil
}

// The kaniko task's ECS events still take the ECS path.
func TestHandleEvent_ECSTaskEvent(t *testing.T) {
	db := &recordingDynamoDB{}
	h := NewDeployTaskStateChangeHandler(fakeECS{}, db, testApplicationsTable, testDeploymentsTable)
	event := []byte(`{
		"source": "aws.ecs",
		"detail-type": "ECS Task State Change",
		"detail": {
			"taskArn": "arn:aws:ecs:us-east-1:123456789012:task/c/abc",
			"clusterArn": "arn:aws:ecs:us-east-1:123456789012:cluster/c",
			"lastStatus": "STOPPED",
			"desiredStatus": "STOPPED",
			"version": 4,
			"containers": [{"exitCode": 1}]
		}
	}`)
	require.NoError(t, h.HandleEvent(context.Background(), event))
	values := setValues(db.tableUpdates(testDeploymentsTable)[0])
	assert.Equal(t, &types.AttributeValueMemberBOOL{Value: true}, values[models.DeploymentErroredField])
	assert.Equal(t, "error", stringValue(t, setValues(db.tableUpdates(testApplicationsTable)[0])[models.ApplicationStatusField]))
}

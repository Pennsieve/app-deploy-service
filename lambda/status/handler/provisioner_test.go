package handler

import (
	"context"
	"fmt"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/pennsieve/app-deploy-service/status/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testProvisionerGroup = "family:dev-app-deploy-service-app-provisioner-task-use1"

func provisionerEvent(group, lastStatus, extra string) []byte {
	return []byte(fmt.Sprintf(`{
		"source": "aws.ecs",
		"detail-type": "ECS Task State Change",
		"detail": {
			"taskArn": "arn:aws:ecs:us-east-1:123456789012:task/c/abc",
			"clusterArn": "arn:aws:ecs:us-east-1:123456789012:cluster/c",
			"group": %q,
			"lastStatus": %q,
			"desiredStatus": "STOPPED",
			"version": 3%s
		}
	}`, group, lastStatus, extra))
}

// A provisioner task that never ran (Docker Hub answering 502 on its image)
// started no build: the version is marked failed rather than left registering.
func TestHandleEvent_ProvisionerFailedToStart(t *testing.T) {
	db := &recordingDynamoDB{}
	h := NewDeployTaskStateChangeHandler(fakeECS{}, db, testApplicationsTable, testDeploymentsTable).WithProvisionerGroup(testProvisionerGroup)
	event := provisionerEvent(testProvisionerGroup, "STOPPED", `,
			"stopCode": "TaskFailedToStart",
			"stoppedReason": "CannotPullContainerError: ... 502 Bad Gateway",
			"containers": [{"lastStatus": "STOPPED"}]`)

	require.NoError(t, h.HandleEvent(context.Background(), event))
	values := setValues(db.tableUpdates(testDeploymentsTable)[0])
	assert.Equal(t, &types.AttributeValueMemberBOOL{Value: true}, values[models.DeploymentErroredField])
	assert.Equal(t, "CannotPullContainerError: ... 502 Bad Gateway", stringValue(t, values[models.DeploymentStoppedReasonField]))
	assert.Equal(t, "error", stringValue(t, setValues(db.tableUpdates(testApplicationsTable)[0])[models.ApplicationStatusField]))
}

// A provisioner task that failed while running (it exits 1 when it can't
// start the build) is a failure too.
func TestHandleEvent_ProvisionerExitedWithError(t *testing.T) {
	db := &recordingDynamoDB{}
	h := NewDeployTaskStateChangeHandler(fakeECS{}, db, testApplicationsTable, testDeploymentsTable).WithProvisionerGroup(testProvisionerGroup)

	require.NoError(t, h.HandleEvent(context.Background(), provisionerEvent(testProvisionerGroup, "STOPPED", `,
			"stopCode": "EssentialContainerExited",
			"containers": [{"exitCode": 1}]`)))
	assert.Equal(t, "error", stringValue(t, setValues(db.tableUpdates(testApplicationsTable)[0])[models.ApplicationStatusField]))
}

// Otherwise the provisioner task only started the build: CodeBuild says
// deployed, so its running and clean-exit events change nothing.
func TestHandleEvent_ProvisionerSuccessIsIgnored(t *testing.T) {
	for name, event := range map[string][]byte{
		"running": provisionerEvent(testProvisionerGroup, "RUNNING", ""),
		"exited":  provisionerEvent(testProvisionerGroup, "STOPPED", `, "stopCode": "EssentialContainerExited", "containers": [{"exitCode": 0}]`),
	} {
		db := &recordingDynamoDB{}
		h := NewDeployTaskStateChangeHandler(fakeECS{}, db, testApplicationsTable, testDeploymentsTable).WithProvisionerGroup(testProvisionerGroup)
		require.NoError(t, h.HandleEvent(context.Background(), event), name)
		assert.Empty(t, db.tableUpdates(testDeploymentsTable), name)
		assert.Empty(t, db.tableUpdates(testApplicationsTable), name)
	}
}

// The deployer task's events are recorded as before, success included.
func TestHandleEvent_DeployerTaskUnchanged(t *testing.T) {
	db := &recordingDynamoDB{}
	h := NewDeployTaskStateChangeHandler(fakeECS{}, db, testApplicationsTable, testDeploymentsTable).WithProvisionerGroup(testProvisionerGroup)

	require.NoError(t, h.HandleEvent(context.Background(), provisionerEvent("family:dev-app-deploy-service-app-deployer-task-use1", "STOPPED", `,
			"stopCode": "EssentialContainerExited",
			"containers": [{"exitCode": 0}]`)))
	assert.Equal(t, "deployed", stringValue(t, setValues(db.tableUpdates(testApplicationsTable)[0])[models.ApplicationStatusField]))
}

func TestDetailErrored_TaskFailedToStart(t *testing.T) {
	assert.True(t, models.Detail{StopCode: models.StopCodeTaskFailedToStart, Containers: []models.Container{{}}}.Errored())
	assert.False(t, models.Detail{StopCode: "EssentialContainerExited", Containers: []models.Container{{ExitCode: 0}}}.Errored())
}

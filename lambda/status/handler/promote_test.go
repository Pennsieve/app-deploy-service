package handler

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/codebuild"
	cbtypes "github.com/aws/aws-sdk-go-v2/service/codebuild/types"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/pennsieve/app-deploy-service/status/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testStaged = "123.dkr.ecr.us-east-1.amazonaws.com/dev-appstore-staging-use1:deployment-1"
	testTarget = "123.dkr.ecr.us-east-1.amazonaws.com/dev-appstore-private-use1:42-v1.0.0"
)

// stagedCodeBuild describes a build that pushed to staging and names its
// App Store image in PROMOTE_TO (the ids come from the build, not the event).
type stagedCodeBuild struct{ promoteTo string }

func (f *stagedCodeBuild) BatchGetBuilds(_ context.Context, in *codebuild.BatchGetBuildsInput, _ ...func(*codebuild.Options)) (*codebuild.BatchGetBuildsOutput, error) {
	vars := []cbtypes.EnvironmentVariable{
		{Name: aws.String("DEPLOYMENT_ID"), Value: aws.String("deployment-1"), Type: cbtypes.EnvironmentVariableTypePlaintext},
		{Name: aws.String("APPLICATION_ID"), Value: aws.String("version-1"), Type: cbtypes.EnvironmentVariableTypePlaintext},
		{Name: aws.String("APPLICATIONS_TABLE"), Value: aws.String(testVersionsTable), Type: cbtypes.EnvironmentVariableTypePlaintext},
		{Name: aws.String("DESTINATION"), Value: aws.String(testStaged), Type: cbtypes.EnvironmentVariableTypePlaintext},
	}
	if f.promoteTo != "" {
		vars = append(vars, cbtypes.EnvironmentVariable{Name: aws.String("PROMOTE_TO"), Value: aws.String(f.promoteTo), Type: cbtypes.EnvironmentVariableTypePlaintext})
	}
	return &codebuild.BatchGetBuildsOutput{Builds: []cbtypes.Build{{Arn: aws.String(in.Ids[0]), Environment: &cbtypes.ProjectEnvironment{EnvironmentVariables: vars}}}}, nil
}

type fakeCopier struct {
	copies [][2]string
	err    error
}

func (f *fakeCopier) Copy(_ context.Context, src, dst string) error {
	f.copies = append(f.copies, [2]string{src, dst})
	return f.err
}

func newStagedTestHandler(promoteTo string, copier *fakeCopier) (*DeployTaskStateChangeHandler, *recordingDynamoDB, *fakeECR) {
	db, ecrApi := &recordingDynamoDB{}, &fakeECR{}
	h := NewDeployTaskStateChangeHandler(nil, db, testApplicationsTable, testDeploymentsTable).
		WithCodeBuild(&stagedCodeBuild{promoteTo: promoteTo}, &fakeLogs{}, &fakeSSM{}).
		WithECR(ecrApi)
	if copier != nil {
		h = h.WithImageCopier(copier)
	}
	return h, db, ecrApi
}

func TestPromotionOf(t *testing.T) {
	env := models.BuildEnvironment{EnvironmentVariables: []models.BuildEnvironmentVariable{
		{Name: "DESTINATION", Value: testStaged}, {Name: "PROMOTE_TO", Value: " " + testTarget + " "},
	}}
	p, ok := promotionOf(env)
	require.True(t, ok)
	assert.Equal(t, promotion{Staged: testStaged, Target: testTarget}, p)

	_, ok = promotionOf(models.BuildEnvironment{EnvironmentVariables: []models.BuildEnvironmentVariable{{Name: "DESTINATION", Value: testTarget}}})
	assert.False(t, ok, "a build that pushed straight to its destination is not promoted")
}

func TestSplitImage(t *testing.T) {
	registry, repo, tag, ok := splitImage(testStaged)
	require.True(t, ok)
	assert.Equal(t, "123.dkr.ecr.us-east-1.amazonaws.com", registry)
	assert.Equal(t, "dev-appstore-staging-use1", repo)
	assert.Equal(t, "deployment-1", tag)
	_, _, _, ok = splitImage("no-registry:tag")
	assert.False(t, ok)
}

func TestHandleEvent_StagedBuildIsPromotedThenDeployed(t *testing.T) {
	copier := &fakeCopier{}
	h, db, ecrApi := newStagedTestHandler(testTarget, copier)
	require.NoError(t, h.HandleEvent(context.Background(), buildEvent(models.BuildSucceeded, false)))

	assert.Equal(t, [][2]string{{testStaged, testTarget}}, copier.copies, "the staged image is copied to its App Store destination")
	require.Len(t, ecrApi.deleted, 1, "the staging tag is deleted after promotion")
	assert.Equal(t, "dev-appstore-staging-use1", aws.ToString(ecrApi.deleted[0].RepositoryName))
	assert.Equal(t, "deployment-1", aws.ToString(ecrApi.deleted[0].ImageIds[0].ImageTag))

	deployments := db.tableUpdates(testDeploymentsTable)
	require.Len(t, deployments, 1)
	values := setValues(deployments[0])
	assert.Equal(t, models.BuildSucceeded, stringValue(t, values[models.DeploymentStopCodeField]))
	assert.Equal(t, &types.AttributeValueMemberBOOL{Value: false}, values[models.DeploymentErroredField])
	versions := db.tableUpdates(testVersionsTable)
	require.Len(t, versions, 1)
	assert.Equal(t, "deployed", stringValue(t, setValues(versions[0])[models.ApplicationStatusField]))
}

func TestHandleEvent_PromotionFailureFailsTheBuild(t *testing.T) {
	copier := &fakeCopier{err: errors.New("registry refused the manifest")}
	h, db, ecrApi := newStagedTestHandler(testTarget, copier)
	require.NoError(t, h.HandleEvent(context.Background(), buildEvent(models.BuildSucceeded, false)))

	assert.Empty(t, ecrApi.deleted, "a failed promotion keeps the staged image for a look")
	deployments := db.tableUpdates(testDeploymentsTable)
	require.Len(t, deployments, 1)
	values := setValues(deployments[0])
	assert.Equal(t, StopCodePromoteFailed, stringValue(t, values[models.DeploymentStopCodeField]))
	assert.Equal(t, &types.AttributeValueMemberBOOL{Value: true}, values[models.DeploymentErroredField])
	assert.Contains(t, stringValue(t, values[models.DeploymentStoppedReasonField]), "could not be promoted")
	assert.Contains(t, stringValue(t, values[models.DeploymentStoppedReasonField]), "registry refused the manifest")
	versions := db.tableUpdates(testVersionsTable)
	require.Len(t, versions, 1)
	assert.Contains(t, stringValue(t, setValues(versions[0])[models.ApplicationStatusField]), "error")
}

func TestHandleEvent_StagedBuildWithoutCopierFails(t *testing.T) {
	h, db, _ := newStagedTestHandler(testTarget, nil)
	require.NoError(t, h.HandleEvent(context.Background(), buildEvent(models.BuildSucceeded, false)))
	values := setValues(db.tableUpdates(testDeploymentsTable)[0])
	assert.Equal(t, StopCodePromoteFailed, stringValue(t, values[models.DeploymentStopCodeField]))
}

func TestHandleEvent_UnstagedBuildIsNotPromoted(t *testing.T) {
	copier := &fakeCopier{}
	h, db, ecrApi := newStagedTestHandler("", copier)
	require.NoError(t, h.HandleEvent(context.Background(), buildEvent(models.BuildSucceeded, false)))
	assert.Empty(t, copier.copies)
	assert.Empty(t, ecrApi.deleted)
	assert.Equal(t, "deployed", stringValue(t, setValues(db.tableUpdates(testVersionsTable)[0])[models.ApplicationStatusField]))
}

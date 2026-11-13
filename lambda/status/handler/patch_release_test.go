package handler

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/pennsieve/app-deploy-service/status/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// queryingDynamoDB is recordingDynamoDB that also answers the versions
// index query with the given versions (tag -> sample status, "" = none).
type queryingDynamoDB struct {
	*recordingDynamoDB
	versions map[string]string
	queries  []*dynamodb.QueryInput
}

func (q *queryingDynamoDB) Query(_ context.Context, in *dynamodb.QueryInput, _ ...func(*dynamodb.Options)) (*dynamodb.QueryOutput, error) {
	q.queries = append(q.queries, in)
	var items []map[string]types.AttributeValue
	for tag, status := range q.versions {
		item := map[string]types.AttributeValue{
			"uuid":    &types.AttributeValueMemberS{Value: "uuid-" + tag},
			"version": &types.AttributeValueMemberS{Value: tag},
		}
		if status != "" {
			item["sample"] = &types.AttributeValueMemberM{Value: map[string]types.AttributeValue{"status": &types.AttributeValueMemberS{Value: status}}}
		}
		items = append(items, item)
	}
	return &dynamodb.QueryOutput{Items: items}, nil
}

// patchTestHandler is a sample-test handler for release tag of app-1 with
// the app's other versions.
func patchTestHandler(tag string, others map[string]string) (*DeployTaskStateChangeHandler, *queryingDynamoDB, *fakeFixtureCodeBuild, *fakeS3, *fakeEvents) {
	items := sampleItems(4096, []string{"standard"}, nil)
	items[testVersionsTable]["version"] = &types.AttributeValueMemberS{Value: tag}
	h, db, cb, store, ev := newSampleTestHandler(items)
	q := &queryingDynamoDB{recordingDynamoDB: db, versions: others}
	h.DynamoDBApi = q
	store.objects = map[string]string{"fixtures/app-1/v1.0.0/manifest.json": readyManifest()}
	return h, q, cb, store, ev
}

func TestReleaseNumbers(t *testing.T) {
	for tag, want := range map[string]bool{"v1.2.3": true, "1.2.3": true, "v0.1.10": true, "v1.2": false, "v1.2.3-rc1": false, "v1.2.3+b": false, "v01.2.3": false, "latest": false} {
		_, ok := releaseNumbers(tag)
		assert.Equal(t, want, ok, tag)
	}
}

func TestPatchRelease_SkipsTheSampleRunWhenTheLineHasAReport(t *testing.T) {
	h, q, cb, _, ev := patchTestHandler("v1.0.3", map[string]string{
		"v1.0.0": models.SampleDocReady, "v1.0.2": models.SampleDocReady, "v1.0.3": "", "v0.9.9": models.SampleDocReady, "v1.0.4": models.SampleDocReady,
	})
	require.NoError(t, h.HandleEvent(context.Background(), fixtureBuildEvent(models.BuildSucceeded)))

	for _, s := range cb.starts {
		assert.NotEqual(t, testSampleProject, aws.ToString(s.ProjectName), "the sample runner is not started")
	}
	require.Len(t, q.queries, 1)
	assert.Equal(t, VersionsByApplicationIndex, aws.ToString(q.queries[0].IndexName))
	rec := sampleRunRecorded(t, q.recordingDynamoDB)
	require.NotNil(t, rec)
	assert.Equal(t, models.SampleSkipped, rec.Status)
	assert.Equal(t, models.SampleErrorPatchRelease, rec.Error.Code)
	assert.Contains(t, rec.Error.Message, "v1.0.2", "the newest earlier release of the line with a report")
	assert.Equal(t, "v1.0.2", rec.ReportOf, "readers that look the report up by tag show v1.0.2's")
	details := ev.details(t)
	require.Len(t, details, 1, "the release event goes out at once with the skipped run")
	require.NotNil(t, details[0].SampleRun)
	assert.Equal(t, models.SampleErrorPatchRelease, details[0].SampleRun.Error.Code)
	assert.Equal(t, "v1.0.2", details[0].SampleRun.ReportOf, "the event names the report that stands")
}

func TestPatchRelease_RunsWhenItIsNotAPatchOrTheLineHasNoReport(t *testing.T) {
	for _, tc := range []struct {
		name, tag string
		others    map[string]string
	}{
		{"minor release", "v1.1.0", map[string]string{"v1.0.5": models.SampleDocReady}},
		{"major release", "v2.0.0", map[string]string{"v1.9.0": models.SampleDocReady}},
		{"pre-release", "v1.0.3-rc1", map[string]string{"v1.0.2": models.SampleDocReady}},
		{"no report in the line", "v1.0.3", map[string]string{"v1.0.2": models.SampleDocFailed, "v1.0.1": "", "v0.9.0": models.SampleDocReady}},
		{"only a later patch has one", "v1.0.3", map[string]string{"v1.0.4": models.SampleDocReady}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, q, cb, _, ev := patchTestHandler(tc.tag, tc.others)
			require.NoError(t, h.HandleEvent(context.Background(), fixtureBuildEvent(models.BuildSucceeded)))
			started := false
			for _, s := range cb.starts {
				started = started || aws.ToString(s.ProjectName) == testSampleProject
			}
			assert.True(t, started, "the sample run starts")
			rec := sampleRunRecorded(t, q.recordingDynamoDB)
			require.NotNil(t, rec)
			assert.Equal(t, models.SampleRunning, rec.Status)
			assert.Empty(t, ev.entries)
		})
	}
}

func TestPatchRelease_HardwareSkipComesFirst(t *testing.T) {
	items := sampleItems(4096, []string{"gpu"}, nil)
	items[testVersionsTable]["version"] = &types.AttributeValueMemberS{Value: "v1.0.3"}
	h, db, _, store, _ := newSampleTestHandler(items)
	q := &queryingDynamoDB{recordingDynamoDB: db, versions: map[string]string{"v1.0.2": models.SampleDocReady}}
	h.DynamoDBApi = q
	store.objects = map[string]string{"fixtures/app-1/v1.0.0/manifest.json": readyManifest()}
	require.NoError(t, h.HandleEvent(context.Background(), fixtureBuildEvent(models.BuildSucceeded)))
	rec := sampleRunRecorded(t, db)
	require.NotNil(t, rec)
	assert.Equal(t, models.SampleErrorNeedsHardware, rec.Error.Code, "a GPU app keeps its own reason, which offers the node run")
	assert.Equal(t, "v1.0.2", rec.ReportOf, "a GPU app's patch still names the report of its line")
}

func TestPatchRelease_HardwareSkipOfANewLineNamesNoReport(t *testing.T) {
	items := sampleItems(4096, []string{"gpu"}, nil)
	items[testVersionsTable]["version"] = &types.AttributeValueMemberS{Value: "v1.1.0"}
	h, db, _, store, _ := newSampleTestHandler(items)
	h.DynamoDBApi = &queryingDynamoDB{recordingDynamoDB: db, versions: map[string]string{"v1.0.2": models.SampleDocReady}}
	store.objects = map[string]string{"fixtures/app-1/v1.0.0/manifest.json": readyManifest()}
	require.NoError(t, h.HandleEvent(context.Background(), fixtureBuildEvent(models.BuildSucceeded)))
	rec := sampleRunRecorded(t, db)
	require.NotNil(t, rec)
	assert.Equal(t, models.SampleErrorNeedsHardware, rec.Error.Code)
	assert.Empty(t, rec.ReportOf, "a minor release starts a line: no earlier report stands for it")
}

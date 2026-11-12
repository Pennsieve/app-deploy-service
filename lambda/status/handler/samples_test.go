package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/pennsieve/app-deploy-service/status/events"
	"github.com/pennsieve/app-deploy-service/status/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testSampleProject     = "dev-app-deploy-service-sample-runner-use1"
	testSampleBuildId     = "arn:aws:codebuild:us-east-1:123456789012:build/dev-app-deploy-service-sample-runner-use1:5amp1e"
	testSampleDestination = "123.dkr.ecr.us-east-1.amazonaws.com/dev-appstore-private-use1:42-v1.0.0"
	testDigest            = "sha256:abc"
)

// sampleItems are the version (with its manifest runtime, review parameters,
// image and, when given, fixtures) and its application.
func sampleItems(memory int, computeTypes []string, fixtures *models.Fixtures) map[string]map[string]types.AttributeValue {
	items := storeItems(true, false, "https://github.com/pennsieve-apps/nwb-inspect")
	version := items[testVersionsTable]
	version["destinationUrl"] = &types.AttributeValueMemberS{Value: testSampleDestination}
	version["imageDigest"] = &types.AttributeValueMemberS{Value: testDigest}
	runtime := map[string]types.AttributeValue{"memory": &types.AttributeValueMemberN{Value: fmt.Sprint(memory)}}
	if computeTypes != nil {
		var l []types.AttributeValue
		for _, t := range computeTypes {
			l = append(l, &types.AttributeValueMemberS{Value: t})
		}
		runtime["computeTypes"] = &types.AttributeValueMemberL{Value: l}
	}
	version["manifest"] = &types.AttributeValueMemberM{Value: map[string]types.AttributeValue{"runtime": &types.AttributeValueMemberM{Value: runtime}}}
	version["review"] = &types.AttributeValueMemberM{Value: map[string]types.AttributeValue{
		"version":    &types.AttributeValueMemberN{Value: "1"},
		"parameters": &types.AttributeValueMemberM{Value: map[string]types.AttributeValue{"N_NEIGHBORS": &types.AttributeValueMemberS{Value: "15"}, "SEED": &types.AttributeValueMemberN{Value: "7"}}},
	}}
	if fixtures != nil {
		av, _ := attributevalue.Marshal(fixtures)
		version["fixtures"] = av
	}
	return items
}

func newSampleTestHandler(items map[string]map[string]types.AttributeValue) (*DeployTaskStateChangeHandler, *recordingDynamoDB, *fakeFixtureCodeBuild, *fakeS3, *fakeEvents) {
	h, db, cb, store, ev := newFixtureTestHandler(items)
	h.Fixtures.SampleProject = testSampleProject
	h.Fixtures.SampleCompute = ParseSampleCompute("BUILD_GENERAL1_MEDIUM=6144,BUILD_GENERAL1_LARGE=14336")
	return h, db, cb, store, ev
}

func sampleRunRecorded(t *testing.T, db *recordingDynamoDB) *models.SampleRun {
	var last *models.SampleRun
	for _, u := range db.tableUpdates(testVersionsTable) {
		if v, ok := setValues(u)[models.SampleRunField]; ok {
			var rec models.SampleRun
			require.NoError(t, attributevalue.Unmarshal(v, &rec))
			last = &rec
		}
	}
	return last
}

func sampleEvents(h *DeployTaskStateChangeHandler) []events.SampleRunStatusEvent {
	var out []events.SampleRunStatusEvent
	for _, p := range h.Realtime.(*fakeRealtime).published {
		if e, ok := p.event.(events.SampleRunStatusEvent); ok {
			out = append(out, e)
		}
	}
	return out
}

func TestParseSampleComputeAndComputeFor(t *testing.T) {
	tiers := ParseSampleCompute(" BUILD_GENERAL1_MEDIUM=6144, BUILD_GENERAL1_LARGE=14336 ,bad,=3")
	require.Equal(t, []ComputeTier{{"BUILD_GENERAL1_MEDIUM", 6144}, {"BUILD_GENERAL1_LARGE", 14336}}, tiers)
	assert.Equal(t, "BUILD_GENERAL1_MEDIUM", computeFor(tiers, 2048))
	assert.Equal(t, "BUILD_GENERAL1_MEDIUM", computeFor(tiers, 6144))
	assert.Equal(t, "BUILD_GENERAL1_LARGE", computeFor(tiers, 8192))
	assert.Equal(t, "", computeFor(tiers, 20000))
	assert.Equal(t, "", computeFor(nil, 1))
	assert.True(t, gpuOnly([]string{"gpu"}))
	assert.False(t, gpuOnly([]string{"standard", "gpu"}))
	assert.False(t, gpuOnly(nil))
}

func TestHandleFixtureBuild_ReadyBundleStartsTheSampleRun(t *testing.T) {
	h, db, cb, store, ev := newSampleTestHandler(sampleItems(4096, []string{"standard"}, nil))
	store.objects = map[string]string{"fixtures/app-1/v1.0.0/manifest.json": readyManifest()}
	require.NoError(t, h.HandleEvent(context.Background(), fixtureBuildEvent(models.BuildSucceeded)))

	require.Len(t, cb.starts, 1, "the sample run starts once the bundle is ready")
	start := cb.starts[0]
	assert.Equal(t, testSampleProject, aws.ToString(start.ProjectName))
	assert.Equal(t, "BUILD_GENERAL1_MEDIUM", string(start.ComputeTypeOverride))
	assert.Equal(t, "123.dkr.ecr.us-east-1.amazonaws.com/dev-appstore-private-use1@sha256:abc", variableValue(start, SampleImageVar))
	assert.Equal(t, testFixtureBundleKey, variableValue(start, SampleBundleKeyVar))
	assert.Equal(t, testStoreAppId, variableValue(start, FixtureApplicationIdVar))
	assert.Equal(t, testFixtureBuildTag, variableValue(start, FixtureTagVar))
	assert.Equal(t, "version-1", variableValue(start, FixtureVersionIdVar))
	assert.Equal(t, "4096", variableValue(start, SampleMemoryVar))
	var params map[string]string
	require.NoError(t, json.Unmarshal([]byte(variableValue(start, SampleParametersVar)), &params))
	assert.Equal(t, map[string]string{"N_NEIGHBORS": "15", "SEED": "7"}, params)

	rec := sampleRunRecorded(t, db)
	require.NotNil(t, rec)
	assert.Equal(t, models.SampleRunning, rec.Status)
	assert.Equal(t, testSampleBuildId, rec.BuildId)
	assert.Equal(t, "BUILD_GENERAL1_MEDIUM", rec.ComputeType)
	assert.Empty(t, ev.entries, "the release event waits for the sample run")
	live := sampleEvents(h)
	require.Len(t, live, 1)
	assert.Equal(t, models.SampleRunning, live[0].Status)
}

func TestHandleFixtureBuild_SampleRunSkippedForHardware(t *testing.T) {
	for _, tc := range []struct {
		name   string
		memory int
		types  []string
		want   string
	}{
		{"gpu only", 4096, []string{"gpu"}, "runs on GPU only"},
		{"too much memory", 20000, []string{"standard"}, "20000 MiB"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, db, cb, store, ev := newSampleTestHandler(sampleItems(tc.memory, tc.types, nil))
			store.objects = map[string]string{"fixtures/app-1/v1.0.0/manifest.json": readyManifest()}
			require.NoError(t, h.HandleEvent(context.Background(), fixtureBuildEvent(models.BuildSucceeded)))
			assert.Empty(t, cb.starts)
			rec := sampleRunRecorded(t, db)
			require.NotNil(t, rec)
			assert.Equal(t, models.SampleSkipped, rec.Status)
			assert.Equal(t, models.SampleErrorNeedsHardware, rec.Error.Code)
			assert.Contains(t, rec.Error.Message, tc.want)
			details := ev.details(t)
			require.Len(t, details, 1, "the release event goes out with the skipped run")
			assert.Equal(t, models.FixturesReady, details[0].Fixtures.Status)
			require.NotNil(t, details[0].SampleRun)
			assert.Equal(t, models.SampleSkipped, details[0].SampleRun.Status)
		})
	}
}

func TestHandleFixtureBuild_FailedFixturesSendTheEventWithoutARun(t *testing.T) {
	h, _, cb, store, ev := newSampleTestHandler(sampleItems(4096, nil, nil))
	store.objects = map[string]string{"fixtures/app-1/v1.0.0/manifest.json": `{"version": 1, "status": "failed", "error": {"code": "no_source_for_port"}, "warnings": [], "files": 0}`}
	require.NoError(t, h.HandleEvent(context.Background(), fixtureBuildEvent(models.BuildSucceeded)))
	assert.Empty(t, cb.starts)
	details := ev.details(t)
	require.Len(t, details, 1)
	assert.Equal(t, models.FixturesFailed, details[0].Fixtures.Status)
	assert.Nil(t, details[0].SampleRun)
}

// sampleRunEvent is a Build State Change event of the sample-runner project.
func sampleRunEvent(status string) []byte {
	complete := status != models.BuildInProgress
	return []byte(fmt.Sprintf(`{
		"version": "0", "id": "e2", "detail-type": "CodeBuild Build State Change", "source": "aws.codebuild",
		"account": "123456789012", "time": "2026-11-11T21:30:00Z", "region": "us-east-1", "resources": ["%[2]s"],
		"detail": {
			"build-status": "%[1]s", "project-name": "%[3]s", "build-id": "%[2]s", "current-phase": "COMPLETED",
			"additional-information": {
				"build-complete": %[4]t, "build-start-time": "Nov 11, 2026 9:20:00 PM",
				"logs": {"group-name": "/aws/codebuild/%[3]s", "stream-name": "5amp1e", "deep-link": "https://console.aws.amazon.com/x"},
				"phases": [{"phase-type": "BUILD", "phase-status": "%[1]s", "phase-context": []}],
				"environment": {"environment-variables": [
					{"name": "IMAGE", "type": "PLAINTEXT", "value": "123.dkr.ecr.us-east-1.amazonaws.com/dev-appstore-private-use1@sha256:abc"},
					{"name": "BUNDLE_KEY", "type": "PLAINTEXT", "value": "%[8]s"},
					{"name": "TAG", "type": "PLAINTEXT", "value": "%[5]s"},
					{"name": "APPLICATION_ID", "type": "PLAINTEXT", "value": "%[6]s"},
					{"name": "VERSION_ID", "type": "PLAINTEXT", "value": "version-1"},
					{"name": "APPLICATIONS_TABLE", "type": "PLAINTEXT", "value": "%[7]s"}
				]}
			}
		}
	}`, status, testSampleBuildId, testSampleProject, complete, testFixtureBuildTag, testStoreAppId, testVersionsTable, testFixtureBundleKey))
}

func readyFixtures() *models.Fixtures {
	return &models.Fixtures{Status: models.FixturesReady, Bucket: testFixturesBucket, Key: testFixtureBundleKey, Bytes: 2256444, Sha256: "abc123", Files: 7, UpdatedAt: "2026-11-11T20:00:00Z"}
}

func TestHandleSampleRun_ReadyResultsAreRecordedAndAnnounced(t *testing.T) {
	running := &models.SampleRun{Status: models.SampleRunning, BuildId: testSampleBuildId, ComputeType: "BUILD_GENERAL1_MEDIUM", UpdatedAt: "2026-11-11T21:20:00Z"}
	items := sampleItems(4096, nil, readyFixtures())
	av, _ := attributevalue.Marshal(running)
	items[testVersionsTable]["sampleRun"] = av
	h, db, _, store, ev := newSampleTestHandler(items)
	exit := 0
	manifest, _ := json.Marshal(models.SampleRunManifest{Version: 1, Status: "ready", Results: &models.SampleResults{Key: "samples/app-1/v1.0.0/r1.zip", Bytes: 512000, Sha256: "r1"},
		Warnings: []string{}, Files: 12, ExitCode: &exit, DurationSeconds: 41, StartedAt: "2026-11-11T21:20:10+00:00", FinishedAt: "2026-11-11T21:20:51+00:00", BuildId: testSampleBuildId, LogTail: "done"})
	store.objects = map[string]string{"samples/app-1/v1.0.0/manifest.json": string(manifest)}
	now := time.Date(2026, 11, 11, 21, 30, 0, 0, time.UTC)
	obj := func(key string, age time.Duration) s3types.Object {
		return s3types.Object{Key: aws.String(key), LastModified: aws.Time(now.Add(-age))}
	}
	store.listed = []s3types.Object{
		obj("samples/app-1/v1.0.0/r1.zip", 0),
		obj("samples/app-1/v0.9.0/r0.zip", 48*time.Hour),
		obj("samples/app-1/v0.8.0/old.zip", 96*time.Hour),
		obj("fixtures/app-1/v0.8.0/old.tar.gz", 96*time.Hour),
	}
	require.NoError(t, h.HandleEvent(context.Background(), sampleRunEvent(models.BuildSucceeded)))

	rec := sampleRunRecorded(t, db)
	require.NotNil(t, rec)
	assert.Equal(t, models.SampleReady, rec.Status)
	assert.Equal(t, "samples/app-1/v1.0.0/r1.zip", rec.Key)
	assert.Equal(t, testFixturesBucket, rec.Bucket)
	assert.Equal(t, int64(512000), rec.Bytes)
	assert.Equal(t, 12, rec.Files)
	assert.Equal(t, 41, rec.DurationSeconds)
	assert.Equal(t, "BUILD_GENERAL1_MEDIUM", rec.ComputeType, "kept from the running record")
	require.NotNil(t, rec.ExitCode)
	assert.Equal(t, 0, *rec.ExitCode)
	assert.Equal(t, map[string]string{"samples/app-1/v0.8.0/old.zip": retainTagNo}, store.tagged, "older sample results expire; fixtures and the previous release stay")

	details := ev.details(t)
	require.Len(t, details, 1)
	assert.Equal(t, models.FixturesReady, details[0].Fixtures.Status)
	assert.Equal(t, testFixtureBundleKey, details[0].Fixtures.Key, "the event carries the fixtures record from the version")
	require.NotNil(t, details[0].SampleRun)
	assert.Equal(t, models.SampleReady, details[0].SampleRun.Status)
	assert.Equal(t, "samples/app-1/v1.0.0/r1.zip", details[0].SampleRun.Key)
	assert.Equal(t, testDigest, details[0].ImageDigest)
	live := sampleEvents(h)
	require.Len(t, live, 1)
	assert.Equal(t, models.SampleReady, live[0].Status)
}

func TestHandleSampleRun_FailedRunAndFailedBuild(t *testing.T) {
	t.Run("the app failed", func(t *testing.T) {
		h, db, _, store, ev := newSampleTestHandler(sampleItems(4096, nil, readyFixtures()))
		exit := 3
		manifest, _ := json.Marshal(models.SampleRunManifest{Version: 1, Status: "failed", Error: &models.FixturesError{Code: "run_failed", Message: "the app exited 3"},
			Warnings: []string{}, ExitCode: &exit, DurationSeconds: 5, LogTail: "Traceback\nValueError: no cells"})
		store.objects = map[string]string{"samples/app-1/v1.0.0/manifest.json": string(manifest)}
		require.NoError(t, h.HandleEvent(context.Background(), sampleRunEvent(models.BuildSucceeded)))
		rec := sampleRunRecorded(t, db)
		require.NotNil(t, rec)
		assert.Equal(t, models.SampleFailed, rec.Status)
		assert.Equal(t, "run_failed", rec.Error.Code)
		assert.Contains(t, rec.Error.Message, "ValueError: no cells")
		assert.Empty(t, store.tagged)
		assert.Equal(t, models.SampleFailed, ev.details(t)[0].SampleRun.Status)
	})
	t.Run("the build timed out", func(t *testing.T) {
		h, db, _, _, ev := newSampleTestHandler(sampleItems(4096, nil, readyFixtures()))
		require.NoError(t, h.HandleEvent(context.Background(), sampleRunEvent(models.BuildTimedOut)))
		rec := sampleRunRecorded(t, db)
		require.NotNil(t, rec)
		assert.Equal(t, "build_timed_out", rec.Error.Code)
		assert.Contains(t, rec.Error.Message, "sample run TIMED_OUT")
		assert.Len(t, ev.details(t), 1)
	})
	t.Run("manifest missing", func(t *testing.T) {
		h, db, _, _, _ := newSampleTestHandler(sampleItems(4096, nil, readyFixtures()))
		require.NoError(t, h.HandleEvent(context.Background(), sampleRunEvent(models.BuildSucceeded)))
		assert.Equal(t, models.SampleErrorManifestMissing, sampleRunRecorded(t, db).Error.Code)
	})
}

func TestHandleSampleRun_InProgressIsIgnored(t *testing.T) {
	h, db, _, _, ev := newSampleTestHandler(sampleItems(4096, nil, readyFixtures()))
	require.NoError(t, h.HandleEvent(context.Background(), sampleRunEvent(models.BuildInProgress)))
	assert.Empty(t, db.updates)
	assert.Empty(t, ev.entries)
}

// An owner's sample run on their compute node, registered by the service
// lambda: the version is recorded from the manifest as a runner's would be,
// keeping the run and node, and the release event goes out.
func TestHandleSampleRegistered_RecordsFromManifestAndAnnounces(t *testing.T) {
	running := &models.SampleRun{Status: models.SampleRunning, RunId: "run-7", ComputeNodeId: "node-1", StartedBy: "N:user:owner", Source: "node", UpdatedAt: "2026-11-12T15:00:00Z"}
	items := sampleItems(4096, nil, readyFixtures())
	av, _ := attributevalue.Marshal(running)
	items[testVersionsTable]["sampleRun"] = av
	h, db, _, store, ev := newSampleTestHandler(items)
	manifest, _ := json.Marshal(models.SampleRunManifest{Version: 1, Status: "ready", Results: &models.SampleResults{Key: "samples/app-1/v1.0.0/r7.zip", Bytes: 4096, Sha256: "r7"},
		Warnings: []string{}, Files: 9, DurationSeconds: 750, StartedAt: "2026-11-12T15:00:00Z", FinishedAt: "2026-11-12T15:12:30Z", BuildId: "run:run-7",
		Node: &models.SampleNode{ComputeNodeId: "node-1", Source: "node"}})
	store.objects = map[string]string{"samples/app-1/v1.0.0/manifest.json": string(manifest)}
	raw, _ := json.Marshal(map[string]any{
		"source": models.ReleaseReadySource, "detail-type": models.SampleRegisteredDetailType,
		"detail": models.SampleRegistered{Environment: "dev", ApplicationId: testStoreAppId, VersionId: "version-1", Tag: testFixtureBuildTag, RunId: "run-7", ComputeNodeId: "node-1", VersionsTable: testVersionsTable},
	})
	require.NoError(t, h.HandleEvent(context.Background(), raw))

	rec := sampleRunRecorded(t, db)
	require.NotNil(t, rec)
	assert.Equal(t, models.SampleReady, rec.Status)
	assert.Equal(t, "samples/app-1/v1.0.0/r7.zip", rec.Key)
	assert.Equal(t, testFixturesBucket, rec.Bucket)
	assert.Equal(t, 9, rec.Files)
	assert.Equal(t, 750, rec.DurationSeconds)
	assert.Equal(t, "run:run-7", rec.BuildId)
	assert.Equal(t, "run-7", rec.RunId)
	assert.Equal(t, "node-1", rec.ComputeNodeId)
	assert.Equal(t, "N:user:owner", rec.StartedBy, "kept from the running record")
	assert.Equal(t, "node", rec.Source)

	details := ev.details(t)
	require.Len(t, details, 1)
	require.NotNil(t, details[0].SampleRun)
	assert.Equal(t, models.SampleReady, details[0].SampleRun.Status)
	assert.Equal(t, "samples/app-1/v1.0.0/r7.zip", details[0].SampleRun.Key)
	assert.Equal(t, "node", details[0].SampleRun.Source)
	live := sampleEvents(h)
	require.Len(t, live, 1)
	assert.Equal(t, models.SampleReady, live[0].Status)
}

func TestHandleSampleRegistered_MissingManifestIsAFailure(t *testing.T) {
	items := sampleItems(4096, nil, readyFixtures())
	h, db, _, _, ev := newSampleTestHandler(items)
	raw, _ := json.Marshal(map[string]any{
		"source": models.ReleaseReadySource, "detail-type": models.SampleRegisteredDetailType,
		"detail": models.SampleRegistered{ApplicationId: testStoreAppId, VersionId: "version-1", Tag: testFixtureBuildTag, RunId: "run-8", VersionsTable: testVersionsTable},
	})
	require.NoError(t, h.HandleEvent(context.Background(), raw))
	rec := sampleRunRecorded(t, db)
	require.NotNil(t, rec)
	assert.Equal(t, models.SampleFailed, rec.Status)
	assert.Equal(t, models.SampleErrorManifestMissing, rec.Error.Code)
	assert.Len(t, ev.details(t), 1, "the release event still goes out, with the failure")
}

package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/codebuild"
	cbtypes "github.com/aws/aws-sdk-go-v2/service/codebuild/types"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/aws-sdk-go-v2/service/eventbridge"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/pennsieve/app-deploy-service/status/events"
	"github.com/pennsieve/app-deploy-service/status/models"
	"github.com/pennsieve/pennsieve-go-core/pkg/realtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testFixtureProject   = "dev-app-deploy-service-fixture-builder-use1"
	testFixturesBucket   = "dev-app-deploy-service-fixtures-use1"
	testStoreAppsTable   = "dev-appstore-applications-table"
	testStoreAppId       = "app-1"
	testFixtureBuildId   = "arn:aws:codebuild:us-east-1:123456789012:build/dev-app-deploy-service-fixture-builder-use1:f1x7ur35"
	testFixtureBuildTag  = "v1.0.0"
	testFixtureBundleKey = "fixtures/app-1/v1.0.0/abc123.tar.gz"
)

type fakeFixtureCodeBuild struct {
	starts []*codebuild.StartBuildInput
	err    error
}

func (f *fakeFixtureCodeBuild) StartBuild(_ context.Context, in *codebuild.StartBuildInput, _ ...func(*codebuild.Options)) (*codebuild.StartBuildOutput, error) {
	f.starts = append(f.starts, in)
	if f.err != nil {
		return nil, f.err
	}
	id := testFixtureBuildId
	if aws.ToString(in.ProjectName) == testSampleProject {
		id = testSampleBuildId
	}
	return &codebuild.StartBuildOutput{Build: &cbtypes.Build{Id: aws.String(id)}}, nil
}

type fakeS3 struct {
	objects map[string]string // key → body
	listed  []s3types.Object
	tagged  map[string]string // key → retain tag
	getErr  error
}

func (f *fakeS3) GetObject(_ context.Context, in *s3.GetObjectInput, _ ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	body, ok := f.objects[aws.ToString(in.Key)]
	if !ok {
		return nil, errors.New("NoSuchKey")
	}
	return &s3.GetObjectOutput{Body: io.NopCloser(strings.NewReader(body))}, nil
}

func (f *fakeS3) ListObjectsV2(_ context.Context, in *s3.ListObjectsV2Input, _ ...func(*s3.Options)) (*s3.ListObjectsV2Output, error) {
	var out []s3types.Object
	for _, o := range f.listed {
		if strings.HasPrefix(aws.ToString(o.Key), aws.ToString(in.Prefix)) {
			out = append(out, o)
		}
	}
	return &s3.ListObjectsV2Output{Contents: out}, nil
}

func (f *fakeS3) PutObjectTagging(_ context.Context, in *s3.PutObjectTaggingInput, _ ...func(*s3.Options)) (*s3.PutObjectTaggingOutput, error) {
	if f.tagged == nil {
		f.tagged = map[string]string{}
	}
	for _, t := range in.Tagging.TagSet {
		if aws.ToString(t.Key) == retainTagKey {
			f.tagged[aws.ToString(in.Key)] = aws.ToString(t.Value)
		}
	}
	return &s3.PutObjectTaggingOutput{}, nil
}

// fakeRealtime records what is published on version channels.
type fakeRealtime struct {
	published []struct {
		channel string
		name    string
		event   any
	}
}

func (f *fakeRealtime) Publish(_ context.Context, channel realtime.Channel, name string, data any) error {
	f.published = append(f.published, struct {
		channel string
		name    string
		event   any
	}{channel.Path(), name, data})
	return nil
}

func (f *fakeRealtime) PublishBatch(_ context.Context, _ []realtime.Event) error { return nil }

func (f *fakeRealtime) fixtureEvents() []events.FixturesStatusEvent {
	var out []events.FixturesStatusEvent
	for _, p := range f.published {
		if e, ok := p.event.(events.FixturesStatusEvent); ok && p.name == events.FixturesStatusEventName {
			out = append(out, e)
		}
	}
	return out
}

type fakeEvents struct{ entries []*eventbridge.PutEventsInput }

func (f *fakeEvents) PutEvents(_ context.Context, in *eventbridge.PutEventsInput, _ ...func(*eventbridge.Options)) (*eventbridge.PutEventsOutput, error) {
	f.entries = append(f.entries, in)
	return &eventbridge.PutEventsOutput{}, nil
}

func (f *fakeEvents) details(t *testing.T) []models.ReleaseReady {
	var out []models.ReleaseReady
	for _, in := range f.entries {
		for _, e := range in.Entries {
			assert.Equal(t, models.ReleaseReadySource, aws.ToString(e.Source))
			assert.Equal(t, models.ReleaseReadyDetailType, aws.ToString(e.DetailType))
			var d models.ReleaseReady
			require.NoError(t, json.Unmarshal([]byte(aws.ToString(e.Detail)), &d))
			out = append(out, d)
		}
	}
	return out
}

// storeItems are the version and its application as the handler reads them.
func storeItems(withReview, isPrivate bool, sourceUrl string) map[string]map[string]types.AttributeValue {
	version := map[string]types.AttributeValue{
		"uuid":          &types.AttributeValueMemberS{Value: "version-1"},
		"applicationId": &types.AttributeValueMemberS{Value: testStoreAppId},
		"version":       &types.AttributeValueMemberS{Value: testFixtureBuildTag},
		"imageDigest":   &types.AttributeValueMemberS{Value: "sha256:abc"},
	}
	if withReview {
		version["review"] = &types.AttributeValueMemberM{Value: map[string]types.AttributeValue{"version": &types.AttributeValueMemberN{Value: "1"}}}
	}
	app := map[string]types.AttributeValue{
		"uuid":       &types.AttributeValueMemberS{Value: testStoreAppId},
		"sourceUrl":  &types.AttributeValueMemberS{Value: sourceUrl},
		"visibility": &types.AttributeValueMemberS{Value: "public"},
		"isPrivate":  &types.AttributeValueMemberBOOL{Value: isPrivate},
	}
	return map[string]map[string]types.AttributeValue{testVersionsTable: version, testStoreAppsTable: app}
}

func newFixtureTestHandler(items map[string]map[string]types.AttributeValue) (*DeployTaskStateChangeHandler, *recordingDynamoDB, *fakeFixtureCodeBuild, *fakeS3, *fakeEvents) {
	db := &recordingDynamoDB{items: items}
	cb, store, events := &fakeFixtureCodeBuild{}, &fakeS3{}, &fakeEvents{}
	h := NewDeployTaskStateChangeHandler(nil, db, testApplicationsTable, testDeploymentsTable).
		WithCodeBuild(&fakeCodeBuild{}, &fakeLogs{}, &fakeSSM{}).
		WithRealtime(&fakeRealtime{}).
		WithFixtureBuilder(&FixtureBuilder{
			Project: testFixtureProject, Bucket: testFixturesBucket, AppStoreApplicationsTable: testStoreAppsTable,
			Environment: "dev", CodeBuild: cb, S3: store, Events: events,
			Now: func() time.Time { return time.Date(2026, 11, 11, 20, 0, 0, 0, time.UTC) },
		})
	return h, db, cb, store, events
}

// fixturesRecorded is the last fixtures attribute an update set on the version.
func fixturesRecorded(t *testing.T, db *recordingDynamoDB) *models.Fixtures {
	var last *models.Fixtures
	for _, u := range db.tableUpdates(testVersionsTable) {
		if v, ok := setValues(u)[models.FixturesField]; ok {
			var rec models.Fixtures
			require.NoError(t, attributevalue.Unmarshal(v, &rec))
			last = &rec
		}
	}
	return last
}

func variableValue(in *codebuild.StartBuildInput, name string) string {
	for _, v := range in.EnvironmentVariablesOverride {
		if aws.ToString(v.Name) == name {
			return aws.ToString(v.Value)
		}
	}
	return ""
}

func TestHandleBuild_DeployedVersionStartsFixtureBuild(t *testing.T) {
	h, db, cb, _, events := newFixtureTestHandler(storeItems(true, false, "https://github.com/pennsieve-apps/nwb-inspect"))
	require.NoError(t, h.HandleEvent(context.Background(), buildEvent(models.BuildSucceeded, true)))

	require.Len(t, cb.starts, 1, "a fixture build is started once the version is deployed")
	start := cb.starts[0]
	assert.Equal(t, testFixtureProject, aws.ToString(start.ProjectName))
	assert.Equal(t, "https://github.com/pennsieve-apps/nwb-inspect", variableValue(start, FixtureRepositoryVar))
	assert.Equal(t, testFixtureBuildTag, variableValue(start, FixtureTagVar))
	assert.Equal(t, testStoreAppId, variableValue(start, FixtureApplicationIdVar))
	assert.Equal(t, "version-1", variableValue(start, FixtureVersionIdVar))
	assert.Equal(t, testVersionsTable, variableValue(start, BuildApplicationsTableVar))

	rec := fixturesRecorded(t, db)
	require.NotNil(t, rec)
	assert.Equal(t, models.FixturesBuilding, rec.Status)
	assert.Equal(t, testFixtureBuildId, rec.BuildId)
	assert.Empty(t, events.entries, "the release event waits for the fixture build")

	live := h.Realtime.(*fakeRealtime).fixtureEvents()
	require.Len(t, live, 1, "the version's channel hears that fixtures are building")
	assert.Equal(t, "version-1", live[0].ApplicationId)
	assert.Equal(t, models.FixturesBuilding, live[0].Status)
	assert.Equal(t, "/applications/version-1", h.Realtime.(*fakeRealtime).published[len(h.Realtime.(*fakeRealtime).published)-1].channel)
}

func TestHandleBuild_SkipsWithoutReviewBlock(t *testing.T) {
	h, db, cb, _, events := newFixtureTestHandler(storeItems(false, false, "https://github.com/pennsieve-apps/st-prep"))
	require.NoError(t, h.HandleEvent(context.Background(), buildEvent(models.BuildSucceeded, true)))
	assert.Empty(t, cb.starts)
	rec := fixturesRecorded(t, db)
	require.NotNil(t, rec)
	assert.Equal(t, models.FixturesSkipped, rec.Status)
	assert.Equal(t, models.FixturesErrorNoReviewBlock, rec.Error.Code)
	live := h.Realtime.(*fakeRealtime).fixtureEvents()
	require.Len(t, live, 1)
	assert.Equal(t, models.FixturesSkipped, live[0].Status)
	assert.Equal(t, models.FixturesErrorNoReviewBlock, live[0].ErrorCode)
	details := events.details(t)
	require.Len(t, details, 1, "the release event goes out at once")
	assert.Equal(t, "version-1", details[0].VersionId)
	assert.Equal(t, testStoreAppId, details[0].ApplicationId)
	assert.Equal(t, "sha256:abc", details[0].ImageDigest)
	assert.Equal(t, models.FixturesSkipped, details[0].Fixtures.Status)
}

func TestHandleBuild_SkipsPrivateRepositoryAndAppFixtures(t *testing.T) {
	for _, tc := range []struct {
		name, repo string
		private    bool
		code       string
	}{
		{"private", "https://github.com/lab/secret-app", true, models.FixturesErrorPrivateRepository},
		{"app-fixtures", "https://github.com/pennsieve-apps/app-fixtures.git", false, models.FixturesErrorPlatformApp},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, db, cb, _, events := newFixtureTestHandler(storeItems(true, tc.private, tc.repo))
			require.NoError(t, h.HandleEvent(context.Background(), buildEvent(models.BuildSucceeded, true)))
			assert.Empty(t, cb.starts)
			rec := fixturesRecorded(t, db)
			require.NotNil(t, rec)
			assert.Equal(t, models.FixturesSkipped, rec.Status)
			assert.Equal(t, tc.code, rec.Error.Code)
			assert.Len(t, events.details(t), 1)
		})
	}
}

func TestHandleBuild_FailedImageBuildStartsNoFixtureBuild(t *testing.T) {
	h, db, cb, _, events := newFixtureTestHandler(storeItems(true, false, "https://github.com/pennsieve-apps/nwb-inspect"))
	require.NoError(t, h.HandleEvent(context.Background(), buildEvent(models.BuildFailed, true)))
	assert.Empty(t, cb.starts)
	assert.Nil(t, fixturesRecorded(t, db))
	assert.Empty(t, events.entries)
}

func TestHandleBuild_NonDefaultVariantStartsNoFixtureBuild(t *testing.T) {
	h, _, cb, _, _ := newFixtureTestHandler(storeItems(true, false, "https://github.com/pennsieve-apps/nwb-inspect"))
	event := strings.Replace(string(buildEvent(models.BuildSucceeded, true)),
		`{"name": "GIT_TOKEN"`,
		`{"name": "VARIANT", "type": "PLAINTEXT", "value": "gpu"}, {"name": "VARIANT_DEFAULT", "type": "PLAINTEXT", "value": "false"}, {"name": "GIT_TOKEN"`, 1)
	require.NoError(t, h.HandleEvent(context.Background(), []byte(event)))
	assert.Empty(t, cb.starts)
}

func TestHandleBuild_StartFailureIsRecorded(t *testing.T) {
	h, db, cb, _, events := newFixtureTestHandler(storeItems(true, false, "https://github.com/pennsieve-apps/nwb-inspect"))
	cb.err = errors.New("AccountLimitExceeded")
	require.NoError(t, h.HandleEvent(context.Background(), buildEvent(models.BuildSucceeded, true)))
	rec := fixturesRecorded(t, db)
	require.NotNil(t, rec)
	assert.Equal(t, models.FixturesFailed, rec.Status)
	assert.Equal(t, models.FixturesErrorBuildFailed, rec.Error.Code)
	assert.Contains(t, rec.Error.Message, "AccountLimitExceeded")
	assert.Len(t, events.details(t), 1)
}

// fixtureBuildEvent is a Build State Change event of the fixture-builder project.
func fixtureBuildEvent(status string) []byte {
	complete := status != models.BuildInProgress
	return []byte(fmt.Sprintf(`{
		"version": "0", "id": "e1", "detail-type": "CodeBuild Build State Change", "source": "aws.codebuild",
		"account": "123456789012", "time": "2026-11-11T20:05:00Z", "region": "us-east-1", "resources": ["%[2]s"],
		"detail": {
			"build-status": "%[1]s", "project-name": "%[3]s", "build-id": "%[2]s", "current-phase": "COMPLETED",
			"additional-information": {
				"build-complete": %[4]t, "build-start-time": "Nov 11, 2026 8:00:00 PM",
				"logs": {"group-name": "/aws/codebuild/%[3]s", "stream-name": "f1x7ur35", "deep-link": "https://console.aws.amazon.com/x"},
				"phases": [{"phase-type": "BUILD", "phase-status": "%[1]s", "phase-context": []}],
				"environment": {"environment-variables": [
					{"name": "REPOSITORY", "type": "PLAINTEXT", "value": "https://github.com/pennsieve-apps/nwb-inspect"},
					{"name": "TAG", "type": "PLAINTEXT", "value": "%[5]s"},
					{"name": "APPLICATION_ID", "type": "PLAINTEXT", "value": "%[6]s"},
					{"name": "VERSION_ID", "type": "PLAINTEXT", "value": "version-1"},
					{"name": "APPLICATIONS_TABLE", "type": "PLAINTEXT", "value": "%[7]s"}
				]}
			}
		}
	}`, status, testFixtureBuildId, testFixtureProject, complete, testFixtureBuildTag, testStoreAppId, testVersionsTable))
}

func readyManifest() string {
	return `{"version": 1, "status": "ready", "bundle": {"key": "` + testFixtureBundleKey + `", "bytes": 2256444, "sha256": "abc123"},
		"warnings": [], "files": 7, "startedAt": "2026-11-11T19:58:00+00:00", "builtAt": "2026-11-11T19:59:00+00:00", "buildId": "` + testFixtureBuildId + `",
		"provenance": {"@context": "https://w3id.org/ro/crate/1.1/context"}}`
}

func TestHandleFixtureBuild_ReadyBundleIsRecordedAndAnnounced(t *testing.T) {
	h, db, _, store, events := newFixtureTestHandler(storeItems(true, false, "https://github.com/pennsieve-apps/nwb-inspect"))
	store.objects = map[string]string{"fixtures/app-1/v1.0.0/manifest.json": readyManifest()}
	now := time.Date(2026, 11, 11, 20, 0, 0, 0, time.UTC)
	obj := func(key string, age time.Duration) s3types.Object {
		return s3types.Object{Key: aws.String(key), LastModified: aws.Time(now.Add(-age))}
	}
	store.listed = []s3types.Object{
		obj(testFixtureBundleKey, 0),
		obj("fixtures/app-1/v1.0.0/manifest.json", 0),
		obj("fixtures/app-1/v0.9.0/old1.tar.gz", 48*time.Hour),
		obj("fixtures/app-1/v0.8.0/old2.tar.gz", 96*time.Hour),
		obj("fixtures/app-1/v0.8.0/old2b.tar.gz", 95*time.Hour),
		obj("fixtures/app-2/v1.0.0/other.tar.gz", 0),
	}
	require.NoError(t, h.HandleEvent(context.Background(), fixtureBuildEvent(models.BuildSucceeded)))

	rec := fixturesRecorded(t, db)
	require.NotNil(t, rec)
	assert.Equal(t, models.FixturesReady, rec.Status)
	assert.Equal(t, testFixturesBucket, rec.Bucket)
	assert.Equal(t, testFixtureBundleKey, rec.Key)
	assert.Equal(t, int64(2256444), rec.Bytes)
	assert.Equal(t, "abc123", rec.Sha256)
	assert.Equal(t, 7, rec.Files)
	assert.Equal(t, "2026-11-11T19:59:00+00:00", rec.BuiltAt)
	assert.Nil(t, rec.Error)
	live := h.Realtime.(*fakeRealtime).fixtureEvents()
	require.Len(t, live, 1)
	assert.Equal(t, models.FixturesReady, live[0].Status)
	assert.Equal(t, int64(2256444), live[0].Bytes)
	assert.Equal(t, 7, live[0].Files)
	require.NotNil(t, live[0].Time)

	assert.Equal(t, map[string]string{
		"fixtures/app-1/v0.8.0/old2.tar.gz":  retainTagNo,
		"fixtures/app-1/v0.8.0/old2b.tar.gz": retainTagNo,
	}, store.tagged, "the current and previous release stay; older bundles are tagged for expiry; other apps untouched")

	details := events.details(t)
	require.Len(t, details, 1)
	assert.Equal(t, "dev", details[0].Environment)
	assert.Equal(t, "version-1", details[0].VersionId)
	assert.Equal(t, testFixtureBuildTag, details[0].Tag)
	assert.Equal(t, "sha256:abc", details[0].ImageDigest)
	assert.Equal(t, "https://github.com/pennsieve-apps/nwb-inspect", details[0].SourceUrl)
	assert.Equal(t, models.FixturesReady, details[0].Fixtures.Status)
	assert.Equal(t, testFixtureBundleKey, details[0].Fixtures.Key)
	assert.Equal(t, []string{"version-1"}, events.entries[0].Entries[0].Resources)
}

func TestHandleFixtureBuild_FixturesFailedInTheManifest(t *testing.T) {
	h, db, _, store, events := newFixtureTestHandler(storeItems(true, false, "https://github.com/pennsieve-apps/nwb-inspect"))
	store.objects = map[string]string{"fixtures/app-1/v1.0.0/manifest.json": `{"version": 1, "status": "failed",
		"error": {"code": "dataset_too_large", "message": "ds000001 is 3 GB"}, "warnings": [], "files": 0,
		"startedAt": "2026-11-11T19:58:00+00:00", "builtAt": "2026-11-11T19:59:00+00:00", "buildId": "` + testFixtureBuildId + `"}`}
	require.NoError(t, h.HandleEvent(context.Background(), fixtureBuildEvent(models.BuildSucceeded)))
	rec := fixturesRecorded(t, db)
	require.NotNil(t, rec)
	assert.Equal(t, models.FixturesFailed, rec.Status)
	assert.Equal(t, "dataset_too_large", rec.Error.Code)
	assert.Empty(t, rec.Key)
	assert.Empty(t, store.tagged, "nothing expires when no bundle was made")
	assert.Equal(t, models.FixturesFailed, events.details(t)[0].Fixtures.Status)
}

func TestHandleFixtureBuild_BuildFailedAndManifestMissing(t *testing.T) {
	t.Run("build failed", func(t *testing.T) {
		h, db, _, _, events := newFixtureTestHandler(storeItems(true, false, "https://github.com/pennsieve-apps/nwb-inspect"))
		require.NoError(t, h.HandleEvent(context.Background(), fixtureBuildEvent(models.BuildTimedOut)))
		rec := fixturesRecorded(t, db)
		require.NotNil(t, rec)
		assert.Equal(t, models.FixturesFailed, rec.Status)
		assert.Equal(t, "build_timed_out", rec.Error.Code)
		assert.Contains(t, rec.Error.Message, "fixture build TIMED_OUT")
		assert.Len(t, events.details(t), 1)
	})
	t.Run("manifest missing", func(t *testing.T) {
		h, db, _, _, events := newFixtureTestHandler(storeItems(true, false, "https://github.com/pennsieve-apps/nwb-inspect"))
		require.NoError(t, h.HandleEvent(context.Background(), fixtureBuildEvent(models.BuildSucceeded)))
		rec := fixturesRecorded(t, db)
		require.NotNil(t, rec)
		assert.Equal(t, models.FixturesFailed, rec.Status)
		assert.Equal(t, models.FixturesErrorManifestMissing, rec.Error.Code)
		assert.Len(t, events.details(t), 1)
	})
}

func TestHandleFixtureBuild_InProgressIsIgnored(t *testing.T) {
	h, db, _, _, events := newFixtureTestHandler(storeItems(true, false, "https://github.com/pennsieve-apps/nwb-inspect"))
	require.NoError(t, h.HandleEvent(context.Background(), fixtureBuildEvent(models.BuildInProgress)))
	assert.Empty(t, db.updates)
	assert.Empty(t, events.entries)
}

func TestHandleBuild_WithoutFixtureBuilderNothingChanges(t *testing.T) {
	h, db, _, _, cb := newBuildTestHandler()
	require.NoError(t, h.HandleEvent(context.Background(), buildEvent(models.BuildSucceeded, true)))
	assert.Nil(t, fixturesRecorded(t, db))
	assert.Equal(t, 0, cb.calls)
}

func TestSameRepository(t *testing.T) {
	assert.True(t, sameRepository("https://github.com/pennsieve-apps/app-fixtures.git", DefaultAppFixturesRepository))
	assert.True(t, sameRepository("https://github.com/Pennsieve-Apps/app-fixtures/", DefaultAppFixturesRepository))
	assert.False(t, sameRepository("https://github.com/pennsieve-apps/app-fixtures-2", DefaultAppFixturesRepository))
}

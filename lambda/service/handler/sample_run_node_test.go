package handler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/eventbridge"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/pennsieve/app-deploy-service/service/models"
	"github.com/pennsieve/app-deploy-service/service/store_dynamodb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	sampleOwner   = "N:user:owner"
	sampleAppUrl  = "https://github.com/pennsieve-apps/spike-sort"
	sampleFxUrl   = "https://github.com/pennsieve-apps/app-fixtures"
	sampleVersion = "ver-1"
)

// --- fakes ---------------------------------------------------------------------

type sampleFakeApps struct {
	byId  map[string]*store_dynamodb.AppStoreApplication
	byUrl map[string][]store_dynamodb.AppStoreApplication
}

func (f *sampleFakeApps) GetBySourceUrl(_ context.Context, u string) ([]store_dynamodb.AppStoreApplication, error) {
	return f.byUrl[u], nil
}
func (f *sampleFakeApps) GetAll(context.Context) ([]store_dynamodb.AppStoreApplication, error) {
	return nil, nil
}
func (f *sampleFakeApps) GetById(_ context.Context, id string) (*store_dynamodb.AppStoreApplication, error) {
	return f.byId[id], nil
}
func (f *sampleFakeApps) Insert(context.Context, store_dynamodb.AppStoreApplication) error {
	return nil
}
func (f *sampleFakeApps) UpdateVisibility(context.Context, string, string) error { return nil }
func (f *sampleFakeApps) UpdateStatus(context.Context, string, models.AppStoreStatus) error {
	return nil
}
func (f *sampleFakeApps) UpdateGithubVisibility(context.Context, string, bool) error { return nil }
func (f *sampleFakeApps) UpdateParams(context.Context, string, []store_dynamodb.AppParameter) error {
	return nil
}
func (f *sampleFakeApps) UpdateSummary(context.Context, string, string, string) error { return nil }

type sampleFakeVersions struct {
	byId    map[string]*store_dynamodb.AppStoreVersion
	byApp   map[string][]store_dynamodb.AppStoreVersion
	written map[string]*store_dynamodb.AppSampleRun
}

func (f *sampleFakeVersions) GetById(_ context.Context, id string) (*store_dynamodb.AppStoreVersion, error) {
	return f.byId[id], nil
}
func (f *sampleFakeVersions) GetByApplicationId(_ context.Context, id string) ([]store_dynamodb.AppStoreVersion, error) {
	return f.byApp[id], nil
}
func (f *sampleFakeVersions) SetSampleRun(_ context.Context, id string, rec *store_dynamodb.AppSampleRun) error {
	if f.written == nil {
		f.written = map[string]*store_dynamodb.AppSampleRun{}
	}
	f.written[id] = rec
	return nil
}

type sampleFakeS3 struct {
	objects map[string][]byte
	copies  []string
	deleted []string
}

func (f *sampleFakeS3) Upload(_ context.Context, _, key string, body io.Reader, _ string) error {
	b, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	if f.objects == nil {
		f.objects = map[string][]byte{}
	}
	f.objects[key] = b
	return nil
}
func (f *sampleFakeS3) CopyObject(_ context.Context, in *s3.CopyObjectInput, _ ...func(*s3.Options)) (*s3.CopyObjectOutput, error) {
	src := strings.SplitN(aws.ToString(in.CopySource), "/", 2)[1]
	f.objects[aws.ToString(in.Key)] = f.objects[src]
	f.copies = append(f.copies, aws.ToString(in.Key))
	return &s3.CopyObjectOutput{}, nil
}
func (f *sampleFakeS3) DeleteObject(_ context.Context, in *s3.DeleteObjectInput, _ ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
	delete(f.objects, aws.ToString(in.Key))
	f.deleted = append(f.deleted, aws.ToString(in.Key))
	return &s3.DeleteObjectOutput{}, nil
}

type sampleFakeEvents struct {
	entries []eventbridge.PutEventsInput
}

func (f *sampleFakeEvents) PutEvents(_ context.Context, in *eventbridge.PutEventsInput, _ ...func(*eventbridge.Options)) (*eventbridge.PutEventsOutput, error) {
	f.entries = append(f.entries, *in)
	return &eventbridge.PutEventsOutput{}, nil
}

func sampleTestService(t *testing.T, version *store_dynamodb.AppStoreVersion) (*sampleRunService, *sampleFakeVersions, *sampleFakeS3, *sampleFakeEvents) {
	t.Helper()
	app := &store_dynamodb.AppStoreApplication{Uuid: "app-1", SourceUrl: sampleAppUrl, OwnerId: sampleOwner}
	fixtures := store_dynamodb.AppStoreApplication{Uuid: "app-fx", SourceUrl: sampleFxUrl, OwnerId: "N:user:pennsieve"}
	versions := &sampleFakeVersions{
		byId: map[string]*store_dynamodb.AppStoreVersion{sampleVersion: version},
		byApp: map[string][]store_dynamodb.AppStoreVersion{"app-fx": {
			{Uuid: "fx-1", ApplicationId: "app-fx", Version: "v0.1.2", Status: "deployed", CreatedAt: "2026-11-11 21:00:00"},
			{Uuid: "fx-2", ApplicationId: "app-fx", Version: "v0.1.3", Status: "deployed", CreatedAt: "2026-11-12 12:00:00"},
			{Uuid: "fx-3", ApplicationId: "app-fx", Version: "v0.1.4", Status: "failed", CreatedAt: "2026-11-12 14:00:00"},
		}},
	}
	store, ev := &sampleFakeS3{objects: map[string][]byte{}}, &sampleFakeEvents{}
	svc := &sampleRunService{
		Apps:     &sampleFakeApps{byId: map[string]*store_dynamodb.AppStoreApplication{"app-1": app}, byUrl: map[string][]store_dynamodb.AppStoreApplication{sampleFxUrl: {fixtures}}},
		Versions: versions,
		Presign: func(_ context.Context, bucket, key string, lifetime time.Duration) (string, error) {
			return "https://" + bucket + ".s3.amazonaws.com/" + key + "?X-Amz-Expires=" + lifetime.String(), nil
		},
		S3: store, Events: ev, Http: http.DefaultClient,
		Bucket: "dev-fixtures", AppFixturesRepository: sampleFxUrl, Environment: "dev", VersionsTable: "dev-versions",
		Now: func() time.Time { return time.Date(2026, 11, 12, 16, 0, 0, 0, time.UTC) },
	}
	return svc, versions, store, ev
}

func skippedVersion() *store_dynamodb.AppStoreVersion {
	v := &store_dynamodb.AppStoreVersion{
		Uuid: sampleVersion, ApplicationId: "app-1", Version: "v0.1.4", Status: "deployed",
		Fixtures:  &store_dynamodb.AppFixtures{Status: "ready", Bucket: "dev-fixtures", Key: "fixtures/app-1/v0.1.4/abc.tar.gz"},
		SampleRun: &store_dynamodb.AppSampleRun{Status: "skipped", Error: &store_dynamodb.AppFixturesError{Code: "needs_hardware", Message: "GPU only"}},
		Review:    &store_dynamodb.AppReview{Version: 1, Parameters: map[string]string{"N_JOBS": "2"}},
	}
	v.Manifest = &store_dynamodb.AppManifest{}
	v.Manifest.Runtime.ComputeTypes = []string{"gpu"}
	return v
}

// --- plan ----------------------------------------------------------------------

func TestSamplePlan_ForTheOwner(t *testing.T) {
	svc, _, _, _ := sampleTestService(t, skippedVersion())
	plan, err := svc.Plan(context.Background(), newTestClaims(sampleOwner, "", nil), "", "app-1", sampleVersion)
	require.NoError(t, err)
	assert.Equal(t, SamplePlanApp{SourceUrl: sampleAppUrl, Tag: "v0.1.4"}, plan.App)
	assert.Equal(t, "https://dev-fixtures.s3.amazonaws.com/fixtures/app-1/v0.1.4/abc.tar.gz?X-Amz-Expires=1h0m0s", plan.Sample.BundleUrl)
	assert.Equal(t, "2026-11-12T17:00:00Z", plan.Sample.ExpiresAt)
	assert.Equal(t, SamplePlanFixtures{SourceUrl: sampleFxUrl, Tag: "v0.1.3"}, plan.Sample.Fixtures, "the newest deployed app-fixtures")
	assert.Equal(t, "spike-sort", plan.StepId)
	assert.Equal(t, map[string]map[string]string{"spike-sort": {"N_JOBS": "2"}}, plan.ProcessorParams)
	assert.True(t, plan.Gpu)
	require.NotNil(t, plan.Reason)
	assert.Equal(t, "needs_hardware", plan.Reason.Code)
}

func TestSamplePlan_Refusals(t *testing.T) {
	ctx := context.Background()
	owner := newTestClaims(sampleOwner, "", nil)

	svc, _, _, _ := sampleTestService(t, skippedVersion())
	_, err := svc.Plan(ctx, newTestClaims("N:user:someone", "", nil), "", "app-1", sampleVersion)
	assertRefusal(t, err, http.StatusForbidden, "not_owner")
	_, err = svc.Plan(ctx, owner, "", "app-1", "ver-9")
	assertRefusal(t, err, http.StatusNotFound, "not_found")
	_, err = svc.Plan(ctx, owner, "", "app-9", sampleVersion)
	assertRefusal(t, err, http.StatusNotFound, "not_found")

	building := skippedVersion()
	building.Fixtures.Status = "building"
	svc, _, _, _ = sampleTestService(t, building)
	_, err = svc.Plan(ctx, owner, "", "app-1", sampleVersion)
	assertRefusal(t, err, http.StatusConflict, "fixtures_not_ready")

	running := skippedVersion()
	running.SampleRun = &store_dynamodb.AppSampleRun{Status: "running", RunId: "run-7", Source: "node"}
	svc, _, _, _ = sampleTestService(t, running)
	_, err = svc.Plan(ctx, owner, "", "app-1", sampleVersion)
	assertRefusal(t, err, http.StatusConflict, "sample_running")

	svc, _, _, _ = sampleTestService(t, skippedVersion())
	svc.Apps.(*sampleFakeApps).byUrl = nil
	_, err = svc.Plan(ctx, owner, "", "app-1", sampleVersion)
	assertRefusal(t, err, http.StatusConflict, "no_app_fixtures")
}

// A node run on record as running is settled from the run itself, with the
// owner's token: failed, the version records it and the plan goes ahead;
// succeeded, the results are what to register; still running, or a run
// workflow-service won't show, keeps the conflict.
func TestSamplePlan_SettlesANodeRunOnRecord(t *testing.T) {
	ctx := context.Background()
	owner := newTestClaims(sampleOwner, "", nil)
	onRecord := func() *store_dynamodb.AppStoreVersion {
		v := skippedVersion()
		v.SampleRun = &store_dynamodb.AppSampleRun{Status: "running", RunId: "run-7", ComputeNodeId: "node-1", Source: "node", StartedBy: sampleOwner, StartedAt: "2026-11-12T15:00:00Z"}
		return v
	}
	plan := func(be *sampleBackends, token string) (*SamplePlan, *sampleFakeVersions, error) {
		srv := be.server(t)
		svc, versions, _, _ := sampleTestService(t, onRecord())
		svc.WorkflowsApiUrl = srv.URL + "/workflows"
		out, err := svc.Plan(ctx, owner, token, "app-1", sampleVersion)
		return out, versions, err
	}
	failed := succeededRun()
	failed["status"], failed["completedAt"] = "FAILED", "2026-11-12T15:02:00Z"

	out, versions, err := plan(&sampleBackends{run: failed}, "Bearer owner-token")
	require.NoError(t, err)
	rec := versions.written[sampleVersion]
	require.NotNil(t, rec, "the failure is recorded on the version")
	assert.Equal(t, "failed", rec.Status)
	assert.Equal(t, "run-7", rec.RunId)
	assert.Equal(t, "node", rec.Source)
	assert.Equal(t, "node-1", rec.ComputeNodeId)
	assert.Equal(t, "2026-11-12T15:02:00Z", rec.FinishedAt)
	assert.Equal(t, "2026-11-12T16:00:00Z", rec.UpdatedAt)
	require.NotNil(t, rec.Error)
	assert.Equal(t, "run_failed", rec.Error.Code)
	require.NotNil(t, out.Reason)
	assert.Equal(t, "run_failed", out.Reason.Code, "the plan says why the last run gave no report")
	assert.Equal(t, "v0.1.4", out.App.Tag)

	_, versions, err = plan(&sampleBackends{run: succeededRun()}, "Bearer owner-token")
	assertRefusal(t, err, http.StatusConflict, "sample_succeeded")
	assert.Nil(t, versions.written[sampleVersion])

	still := succeededRun()
	still["status"] = "RUNNING"
	_, versions, err = plan(&sampleBackends{run: still}, "Bearer owner-token")
	assertRefusal(t, err, http.StatusConflict, "sample_running")
	assert.Nil(t, versions.written[sampleVersion])

	_, _, err = plan(&sampleBackends{run: nil}, "Bearer owner-token")
	assertRefusal(t, err, http.StatusConflict, "sample_running")
	_, _, err = plan(&sampleBackends{run: failed}, "")
	assertRefusal(t, err, http.StatusConflict, "sample_running")
}

func assertRefusal(t *testing.T, err error, status int, code string) {
	t.Helper()
	var refusal *sampleRunError
	require.True(t, errors.As(err, &refusal), "expected a refusal, got %v", err)
	assert.Equal(t, status, refusal.Status)
	assert.Equal(t, code, refusal.Code)
}

func TestSampleStepId(t *testing.T) {
	assert.Equal(t, "st-niches", sampleStepId("https://github.com/pennsieve-apps/st-niches"))
	assert.Equal(t, "processor-synthstrip", sampleStepId("https://github.com/pennsieve-apps/processor-synthstrip.git/"))
	assert.Equal(t, "app", sampleStepId("https://github.com/x/results"))
}

// --- started ------------------------------------------------------------------

func TestSampleStarted_RecordsTheRunOnTheVersion(t *testing.T) {
	svc, versions, _, _ := sampleTestService(t, skippedVersion())
	rec, err := svc.Started(context.Background(), newTestClaims(sampleOwner, "", nil), "app-1", sampleVersion, SampleRunStarted{RunId: "run-7", ComputeNodeId: "node-1"})
	require.NoError(t, err)
	assert.Equal(t, "running", rec.Status)
	assert.Equal(t, "run-7", rec.RunId)
	assert.Equal(t, "node-1", rec.ComputeNodeId)
	assert.Equal(t, sampleOwner, rec.StartedBy)
	assert.Equal(t, "node", rec.Source)
	assert.Equal(t, "2026-11-12T16:00:00Z", rec.StartedAt)
	assert.Same(t, rec, versions.written[sampleVersion])

	_, err = svc.Started(context.Background(), newTestClaims(sampleOwner, "", nil), "app-1", sampleVersion, SampleRunStarted{})
	assertRefusal(t, err, http.StatusBadRequest, "bad_request")
}

// --- results ------------------------------------------------------------------

// A stand-in for workflow-service, download-service and the archive link.
type sampleBackends struct {
	run         map[string]any
	archives    []map[string]any
	archive     []byte
	authSeen    []string
	linkVisited int
}

func (b *sampleBackends) server(t *testing.T) *httptest.Server {
	t.Helper()
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/archive.zip") {
			b.authSeen = append(b.authSeen, r.Header.Get("Authorization"))
		}
		switch {
		case strings.HasPrefix(r.URL.Path, "/workflows/runs/"):
			if b.run == nil {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			_ = json.NewEncoder(w).Encode(b.run)
		case r.URL.Path == "/downloads/runs/archives":
			_ = json.NewEncoder(w).Encode(map[string]any{"downloads": b.archives})
		case strings.HasSuffix(r.URL.Path, "/url"):
			_ = json.NewEncoder(w).Encode(map[string]any{"url": srv.URL + "/archive.zip"})
		case r.URL.Path == "/archive.zip":
			b.linkVisited++
			_, _ = w.Write(b.archive)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func succeededRun() map[string]any {
	return map[string]any{"uuid": "run-7", "status": "SUCCEEDED", "startedAt": "2026-11-12T15:00:00Z", "completedAt": "2026-11-12T15:12:30Z",
		"computeNodeUuid": "node-1", "app": map[string]any{"sourceUrl": sampleAppUrl, "tag": "v0.1.4", "applicationId": "app-1", "sample": true}}
}

func TestSampleResults_RegistersTheArchive(t *testing.T) {
	archive := []byte("PK\x03\x04 a results zip")
	be := &sampleBackends{run: succeededRun(), archive: archive,
		archives: []map[string]any{{"id": "ra_1", "runId": "run-7", "status": "READY", "fileCount": 9, "totalBytes": len(archive)}}}
	srv := be.server(t)
	svc, _, store, ev := sampleTestService(t, skippedVersion())
	svc.WorkflowsApiUrl, svc.DownloadsApiUrl = srv.URL+"/workflows", srv.URL+"/downloads"

	out, err := svc.Results(context.Background(), newTestClaims(sampleOwner, "", nil), "Bearer owner-token", "app-1", sampleVersion, SampleRunResults{RunId: "run-7"})
	require.NoError(t, err)
	sum := sha256.Sum256(archive)
	sha := hex.EncodeToString(sum[:])
	assert.Equal(t, "samples/app-1/v0.1.4/"+sha+".zip", out.Key)
	assert.Equal(t, int64(len(archive)), out.Bytes)
	assert.Equal(t, 9, out.Files)
	assert.Equal(t, "run-7", out.RunId)
	assert.Equal(t, "node-1", out.ComputeNodeId)
	assert.Equal(t, "dev-versions", out.VersionsTable)
	assert.Equal(t, []string{"Bearer owner-token", "Bearer owner-token", "Bearer owner-token"}, be.authSeen, "the owner's own token, to each service")
	assert.Equal(t, 1, be.linkVisited)

	assert.Equal(t, archive, store.objects[out.Key], "the archive, named by its hash")
	assert.Equal(t, []string{"samples/app-1/v0.1.4/staging-run-7.zip"}, store.deleted, "the staging copy is gone")
	var manifest map[string]any
	require.NoError(t, json.Unmarshal(store.objects["samples/app-1/v0.1.4/manifest.json"], &manifest))
	assert.Equal(t, "ready", manifest["status"])
	assert.Equal(t, out.Key, manifest["results"].(map[string]any)["key"])
	assert.Equal(t, float64(9), manifest["files"])
	assert.Equal(t, float64(750), manifest["durationSeconds"])
	assert.Equal(t, "run:run-7", manifest["buildId"])
	assert.Equal(t, map[string]any{"computeNodeId": "node-1", "source": "node"}, manifest["node"])

	require.Len(t, ev.entries, 1)
	entry := ev.entries[0].Entries[0]
	assert.Equal(t, "pennsieve.app-deploy-service", aws.ToString(entry.Source))
	assert.Equal(t, "App Sample Run Registered", aws.ToString(entry.DetailType))
	var detail SampleRegistered
	require.NoError(t, json.Unmarshal([]byte(aws.ToString(entry.Detail)), &detail))
	assert.Equal(t, *out, detail)
}

func TestSampleResults_Refusals(t *testing.T) {
	ctx := context.Background()
	owner := newTestClaims(sampleOwner, "", nil)
	try := func(be *sampleBackends) error {
		srv := be.server(t)
		svc, _, _, _ := sampleTestService(t, skippedVersion())
		svc.WorkflowsApiUrl, svc.DownloadsApiUrl = srv.URL+"/workflows", srv.URL+"/downloads"
		_, err := svc.Results(ctx, owner, "Bearer t", "app-1", sampleVersion, SampleRunResults{RunId: "run-7"})
		return err
	}
	ready := []map[string]any{{"id": "ra_1", "runId": "run-7", "status": "READY", "fileCount": 1, "totalBytes": 10}}

	assertRefusal(t, try(&sampleBackends{run: nil}), http.StatusNotFound, "run_not_found")

	notSample := succeededRun()
	notSample["app"].(map[string]any)["sample"] = false
	assertRefusal(t, try(&sampleBackends{run: notSample, archives: ready}), http.StatusConflict, "not_a_sample_run")

	other := succeededRun()
	other["app"].(map[string]any)["tag"] = "v0.1.3"
	assertRefusal(t, try(&sampleBackends{run: other, archives: ready}), http.StatusConflict, "wrong_version")

	failed := succeededRun()
	failed["status"] = "FAILED"
	assertRefusal(t, try(&sampleBackends{run: failed, archives: ready}), http.StatusConflict, "run_failed")

	running := succeededRun()
	running["status"] = "RUNNING"
	assertRefusal(t, try(&sampleBackends{run: running, archives: ready}), http.StatusConflict, "run_not_finished")

	assertRefusal(t, try(&sampleBackends{run: succeededRun(), archives: nil}), http.StatusConflict, "archive_missing")
	zipping := []map[string]any{{"id": "ra_1", "runId": "run-7", "status": "RUNNING"}}
	assertRefusal(t, try(&sampleBackends{run: succeededRun(), archives: zipping}), http.StatusConflict, "archive_not_ready")
}

func TestNewestDeployedTag(t *testing.T) {
	assert.Equal(t, "", newestDeployedTag(nil))
	assert.Equal(t, "v2", newestDeployedTag([]store_dynamodb.AppStoreVersion{
		{Version: "v1", Status: "deployed", CreatedAt: "2026-01-01"}, {Version: "v2", Status: "deployed", CreatedAt: "2026-02-01"},
		{Version: "v3", Status: "failed", CreatedAt: "2026-03-01"}}))
}

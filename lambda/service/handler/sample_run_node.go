package handler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/eventbridge"
	ebtypes "github.com/aws/aws-sdk-go-v2/service/eventbridge/types"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/pennsieve/app-deploy-service/service/models"
	"github.com/pennsieve/app-deploy-service/service/store_dynamodb"
	"github.com/pennsieve/pennsieve-go-core/pkg/authorizer"
)

// A sample run on the owner's compute node (docs/builds.md, "Sample runs on
// a compute node"): for an app the platform's runner can't run (a GPU app,
// or more memory than CodeBuild has), the owner runs the sample on a node
// of theirs from the app's page. Three steps, each a request of the owner:
//
//   GET  /store/{id}/versions/{versionId}/sample-run/plan     what to send workflow-service
//   POST /store/{id}/versions/{versionId}/sample-run          the run started: {runId, computeNodeId}
//   POST /store/{id}/versions/{versionId}/sample-run/results  the run succeeded: {runId}; its
//                                                             results archive becomes the sample run
//
// The plan presigns the release's fixture bundle and names the app-fixtures
// app, so the app can POST /runs to workflow-service as the owner (an app
// run with sample). Registering the results reads the run from
// workflow-service and its archive from download-service with the owner's
// own token (both answer only the run's creator), copies the archive into
// the fixtures bucket under the sample runner's key scheme, writes the
// manifest the status lambda reads, and tells the status lambda, which
// records the sample run and emits App Release Ready as for a runner's.

const (
	fixturesBucketEnvVar        = "FIXTURES_BUCKET"
	appFixturesRepositoryEnvVar = "APP_FIXTURES_REPOSITORY"
	downloadsApiUrlEnvVar       = "DOWNLOADS_API_URL"
	workflowsApiUrlEnvVar       = "WORKFLOWS_API_URL"
	pennsieveDomainEnvVar       = "PENNSIEVE_DOMAIN"

	defaultAppFixturesRepository = "https://github.com/pennsieve-apps/app-fixtures"
	bundleLinkLifetime           = time.Hour
	// The most a results archive may be: a sample's results are small; the
	// copy streams, this only bounds a surprise.
	maxSampleArchiveBytes = int64(5) << 30
	samplesKeyPrefix      = "samples/"

	// The event the status lambda takes up (lambda/status/handler/samples.go).
	sampleRegisteredSource     = "pennsieve.app-deploy-service"
	sampleRegisteredDetailType = "App Sample Run Registered"

	sampleRunNodeSource = "node"
)

var nonStepId = regexp.MustCompile(`[^a-z0-9-]+`)

// sampleStepId is the app's step id in workflow-service's app run: the
// repository name made safe (workflow-service handler/app_runs.go,
// appRunStepId); the review parameters are keyed by it.
func sampleStepId(sourceUrl string) string {
	name := strings.TrimSuffix(strings.TrimRight(sourceUrl, "/"), ".git")
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	id := strings.Trim(nonStepId.ReplaceAllString(strings.ToLower(name), "-"), "-")
	if id == "" || strings.HasPrefix(id, "input-") || id == "results" || id == "input" {
		return "app"
	}
	return id
}

// SamplePlan is what the app sends workflow-service to start the run.
type SamplePlan struct {
	App             SamplePlanApp                `json:"app"`
	Sample          SamplePlanSample             `json:"sample"`
	StepId          string                       `json:"stepId"`
	ProcessorParams map[string]map[string]string `json:"processorParams"`
	// Why the platform did not run it: the runner's skip, for the dialog.
	Reason *models.AppFixturesError `json:"reason,omitempty"`
	Gpu    bool                     `json:"gpu"`
}

type SamplePlanApp struct {
	SourceUrl string `json:"sourceUrl"`
	Tag       string `json:"tag"`
}

type SamplePlanSample struct {
	BundleUrl string             `json:"bundleUrl"`
	ExpiresAt string             `json:"expiresAt"`
	Fixtures  SamplePlanFixtures `json:"fixtures"`
}

type SamplePlanFixtures struct {
	SourceUrl string `json:"sourceUrl"`
	Tag       string `json:"tag"`
}

// SampleRunStarted is the body of POST .../sample-run.
type SampleRunStarted struct {
	RunId         string `json:"runId"`
	ComputeNodeId string `json:"computeNodeId"`
}

// SampleRunResults is the body of POST .../sample-run/results.
type SampleRunResults struct {
	RunId string `json:"runId"`
}

// SampleRegistered is the response of POST .../sample-run/results and the
// detail of the event the status lambda takes up.
type SampleRegistered struct {
	Environment   string `json:"environment"`
	ApplicationId string `json:"applicationId"`
	VersionId     string `json:"versionId"`
	Tag           string `json:"tag"`
	RunId         string `json:"runId"`
	ComputeNodeId string `json:"computeNodeId,omitempty"`
	Sha256        string `json:"sha256"`
	Bytes         int64  `json:"bytes"`
	Files         int    `json:"files"`
	Key           string `json:"key"`
	VersionsTable string `json:"versionsTable"`
}

// sampleRunService does the work behind the three handlers; its parts are
// interfaces so the tests stand them in.
type sampleRunService struct {
	Apps                  store_dynamodb.AppStoreDBStore
	Versions              sampleVersionStore
	Presign               func(ctx context.Context, bucket, key string, lifetime time.Duration) (string, error)
	S3                    sampleS3Api
	Events                sampleEventsApi
	Http                  *http.Client
	Bucket                string
	AppFixturesRepository string
	WorkflowsApiUrl       string
	DownloadsApiUrl       string
	Environment           string
	VersionsTable         string
	Now                   func() time.Time
}

type sampleVersionStore interface {
	GetById(ctx context.Context, uuid string) (*store_dynamodb.AppStoreVersion, error)
	GetByApplicationId(ctx context.Context, applicationId string) ([]store_dynamodb.AppStoreVersion, error)
	SetSampleRun(ctx context.Context, uuid string, rec *store_dynamodb.AppSampleRun) error
}

type sampleS3Api interface {
	Upload(ctx context.Context, bucket, key string, body io.Reader, contentType string) error
	CopyObject(ctx context.Context, params *s3.CopyObjectInput, optFns ...func(*s3.Options)) (*s3.CopyObjectOutput, error)
	DeleteObject(ctx context.Context, params *s3.DeleteObjectInput, optFns ...func(*s3.Options)) (*s3.DeleteObjectOutput, error)
}

type sampleEventsApi interface {
	PutEvents(ctx context.Context, params *eventbridge.PutEventsInput, optFns ...func(*eventbridge.Options)) (*eventbridge.PutEventsOutput, error)
}

// s3Uploader is sampleS3Api over the SDK: a streamed multipart upload plus
// the plain copy and delete.
type s3Uploader struct {
	*s3.Client
}

func (u s3Uploader) Upload(ctx context.Context, bucket, key string, body io.Reader, contentType string) error {
	_, err := manager.NewUploader(u.Client).Upload(ctx, &s3.PutObjectInput{
		Bucket: aws.String(bucket), Key: aws.String(key), Body: body, ContentType: aws.String(contentType),
	})
	return err
}

// sampleRunServiceFromEnv wires the service for a request; nil with a
// message when the environment has no fixtures bucket (no sample pipeline).
func sampleRunServiceFromEnv(ctx context.Context) (*sampleRunService, error) {
	bucket := strings.TrimSpace(os.Getenv(fixturesBucketEnvVar))
	if bucket == "" {
		return nil, errors.New("this environment has no fixture bundles, so no sample runs")
	}
	cfg, err := awsconfig.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, err
	}
	ddb := dynamodb.NewFromConfig(cfg)
	s3Client := s3.NewFromConfig(cfg)
	presigner := s3.NewPresignClient(s3Client)
	domain := strings.TrimSpace(os.Getenv(pennsieveDomainEnvVar))
	return &sampleRunService{
		Apps:     store_dynamodb.NewAppStoreDatabaseStore(ddb, os.Getenv(appstoreApplicationsTableNameKey)),
		Versions: store_dynamodb.NewAppStoreVersionDatabaseStore(ddb, os.Getenv(appstoreVersionsTableNameKey)),
		Presign: func(ctx context.Context, bucket, key string, lifetime time.Duration) (string, error) {
			out, err := presigner.PresignGetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(bucket), Key: aws.String(key)},
				s3.WithPresignExpires(lifetime))
			if err != nil {
				return "", err
			}
			return out.URL, nil
		},
		S3:                    s3Uploader{s3Client},
		Events:                eventbridge.NewFromConfig(cfg),
		Http:                  &http.Client{Timeout: 10 * time.Minute},
		Bucket:                bucket,
		AppFixturesRepository: envOr(appFixturesRepositoryEnvVar, defaultAppFixturesRepository),
		WorkflowsApiUrl:       envOr(workflowsApiUrlEnvVar, "https://api2."+domain+"/compute/workflows"),
		DownloadsApiUrl:       envOr(downloadsApiUrlEnvVar, "https://api2."+domain+"/downloads"),
		Environment:           os.Getenv("ENV"),
		VersionsTable:         os.Getenv(appstoreVersionsTableNameKey),
		Now:                   func() time.Time { return time.Now().UTC() },
	}, nil
}

func envOr(name, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(name)); v != "" {
		return strings.TrimRight(v, "/")
	}
	return fallback
}

// sampleRunError is a refusal with its HTTP status and a code for the app.
type sampleRunError struct {
	Status  int
	Code    string
	Message string
}

func (e *sampleRunError) Error() string { return e.Message }

func refuse(status int, code, message string) error {
	return &sampleRunError{Status: status, Code: code, Message: message}
}

// ownedVersion is the version of the app, for its owner: 404 for an unknown
// app or a version of another app, 403 for anyone but the owner.
func (s *sampleRunService) ownedVersion(ctx context.Context, claims *authorizer.Claims, appId, versionId string) (*store_dynamodb.AppStoreApplication, *store_dynamodb.AppStoreVersion, error) {
	app, err := s.Apps.GetById(ctx, appId)
	if err != nil {
		return nil, nil, err
	}
	if app == nil {
		return nil, nil, refuse(http.StatusNotFound, "not_found", "no such app")
	}
	if claims == nil || !IsAppOwner(ctx, claims, app) {
		return nil, nil, refuse(http.StatusForbidden, "not_owner", "only the app's owner runs its sample")
	}
	version, err := s.Versions.GetById(ctx, versionId)
	if err != nil {
		return nil, nil, err
	}
	if version == nil || version.ApplicationId != app.Uuid {
		return nil, nil, refuse(http.StatusNotFound, "not_found", "no such version of this app")
	}
	return app, version, nil
}

// Plan is what the owner's app sends workflow-service: the app version, a
// presigned link to its fixture bundle, the app-fixtures app at its newest
// version, and the release's review parameters for the app's step.
func (s *sampleRunService) Plan(ctx context.Context, claims *authorizer.Claims, appId, versionId string) (*SamplePlan, error) {
	app, version, err := s.ownedVersion(ctx, claims, appId, versionId)
	if err != nil {
		return nil, err
	}
	if version.Status != "deployed" {
		return nil, refuse(http.StatusConflict, "not_deployed", "the version's image is not deployed yet")
	}
	fx := version.Fixtures
	if fx == nil || fx.Status != "ready" || fx.Key == "" {
		return nil, refuse(http.StatusConflict, "fixtures_not_ready", "the release's test inputs are not built; a sample run needs them")
	}
	if sr := version.SampleRun; sr != nil && sr.Status == "running" && sr.RunId != "" {
		return nil, refuse(http.StatusConflict, "sample_running", "a sample run of this version is under way on a compute node (run "+sr.RunId+")")
	}
	fixtures, err := s.newestAppFixtures(ctx)
	if err != nil {
		return nil, err
	}
	bucket := fx.Bucket
	if bucket == "" {
		bucket = s.Bucket
	}
	link, err := s.Presign(ctx, bucket, fx.Key, bundleLinkLifetime)
	if err != nil {
		return nil, fmt.Errorf("presigning the fixture bundle: %w", err)
	}
	stepId := sampleStepId(app.SourceUrl)
	params := map[string]string{}
	if version.Review != nil {
		for k, v := range version.Review.Parameters {
			params[k] = v
		}
	}
	plan := &SamplePlan{
		App:             SamplePlanApp{SourceUrl: app.SourceUrl, Tag: version.Version},
		Sample:          SamplePlanSample{BundleUrl: link, ExpiresAt: s.Now().Add(bundleLinkLifetime).Format(time.RFC3339), Fixtures: fixtures},
		StepId:          stepId,
		ProcessorParams: map[string]map[string]string{stepId: params},
		Gpu:             version.Manifest != nil && gpuOnlyRuntime(version.Manifest),
	}
	if version.SampleRun != nil && version.SampleRun.Error != nil {
		plan.Reason = &models.AppFixturesError{Code: version.SampleRun.Error.Code, Message: version.SampleRun.Error.Message}
	}
	return plan, nil
}

// gpuOnlyRuntime: every compute type the app.yml declares is gpu.
func gpuOnlyRuntime(m *store_dynamodb.AppManifest) bool {
	if m == nil || len(m.Runtime.ComputeTypes) == 0 {
		return false
	}
	for _, t := range m.Runtime.ComputeTypes {
		if strings.ToLower(strings.TrimSpace(t)) != "gpu" {
			return false
		}
	}
	return true
}

// newestAppFixtures is the app-fixtures app and its newest deployed version.
func (s *sampleRunService) newestAppFixtures(ctx context.Context) (SamplePlanFixtures, error) {
	apps, err := s.Apps.GetBySourceUrl(ctx, s.AppFixturesRepository)
	if err != nil {
		return SamplePlanFixtures{}, err
	}
	if len(apps) == 0 {
		return SamplePlanFixtures{}, refuse(http.StatusConflict, "no_app_fixtures", "the app-fixtures app is not in this App Store")
	}
	versions, err := s.Versions.GetByApplicationId(ctx, apps[0].Uuid)
	if err != nil {
		return SamplePlanFixtures{}, err
	}
	tag := newestDeployedTag(versions)
	if tag == "" {
		return SamplePlanFixtures{}, refuse(http.StatusConflict, "no_app_fixtures", "the app-fixtures app has no deployed version")
	}
	return SamplePlanFixtures{SourceUrl: apps[0].SourceUrl, Tag: tag}, nil
}

// newestDeployedTag is the tag of the newest deployed version (by createdAt).
func newestDeployedTag(versions []store_dynamodb.AppStoreVersion) string {
	deployed := versions[:0:0]
	for _, v := range versions {
		if v.Status == "deployed" && v.Version != "" {
			deployed = append(deployed, v)
		}
	}
	sort.Slice(deployed, func(i, j int) bool { return deployed[i].CreatedAt > deployed[j].CreatedAt })
	if len(deployed) == 0 {
		return ""
	}
	return deployed[0].Version
}

// Started records the run the owner started: the version's sample run is
// running on their node until its results are registered.
func (s *sampleRunService) Started(ctx context.Context, claims *authorizer.Claims, appId, versionId string, body SampleRunStarted) (*store_dynamodb.AppSampleRun, error) {
	_, version, err := s.ownedVersion(ctx, claims, appId, versionId)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(body.RunId) == "" {
		return nil, refuse(http.StatusBadRequest, "bad_request", "runId is required")
	}
	now := s.Now().Format(time.RFC3339)
	rec := &store_dynamodb.AppSampleRun{
		Status: "running", RunId: body.RunId, ComputeNodeId: body.ComputeNodeId, StartedBy: claims.UserClaim.NodeId,
		Source: sampleRunNodeSource, StartedAt: now, UpdatedAt: now,
	}
	if err := s.Versions.SetSampleRun(ctx, version.Uuid, rec); err != nil {
		return nil, err
	}
	return rec, nil
}

// workflowRun is what app-deploy-service reads of a run (workflow-service
// GET /runs/{runId}).
type workflowRun struct {
	Uuid            string `json:"uuid"`
	Status          string `json:"status"`
	StartedAt       string `json:"startedAt"`
	CompletedAt     string `json:"completedAt"`
	ComputeNodeUuid string `json:"computeNodeUuid"`
	App             *struct {
		SourceUrl     string `json:"sourceUrl"`
		Tag           string `json:"tag"`
		ApplicationId string `json:"applicationId"`
		Sample        bool   `json:"sample"`
	} `json:"app"`
}

// runArchive is a run's results archive as download-service lists it.
type runArchive struct {
	Id         string `json:"id"`
	RunId      string `json:"runId"`
	Status     string `json:"status"`
	FileCount  int    `json:"fileCount"`
	TotalBytes int64  `json:"totalBytes"`
}

// Results registers a succeeded sample run's archive as the version's
// sample run. authorization is the owner's own token: workflow-service and
// download-service answer only the run's creator.
func (s *sampleRunService) Results(ctx context.Context, claims *authorizer.Claims, authorization, appId, versionId string, body SampleRunResults) (*SampleRegistered, error) {
	app, version, err := s.ownedVersion(ctx, claims, appId, versionId)
	if err != nil {
		return nil, err
	}
	runId := strings.TrimSpace(body.RunId)
	if runId == "" {
		return nil, refuse(http.StatusBadRequest, "bad_request", "runId is required")
	}
	run, err := s.readRun(ctx, authorization, runId)
	if err != nil {
		return nil, err
	}
	if run.App == nil || !run.App.Sample {
		return nil, refuse(http.StatusConflict, "not_a_sample_run", "the run is not a sample run of an app")
	}
	if run.App.ApplicationId != app.Uuid || run.App.Tag != version.Version {
		return nil, refuse(http.StatusConflict, "wrong_version", fmt.Sprintf("the run is of %s %s, not this version", run.App.SourceUrl, run.App.Tag))
	}
	switch run.Status {
	case "SUCCEEDED":
	case "FAILED", "CANCELED", "CANCELLED":
		return nil, refuse(http.StatusConflict, "run_failed", "the run did not succeed; nothing to register")
	default:
		return nil, refuse(http.StatusConflict, "run_not_finished", "the run has not finished ("+run.Status+")")
	}
	archive, err := s.readArchive(ctx, authorization, runId)
	if err != nil {
		return nil, err
	}
	link, err := s.archiveLink(ctx, authorization, archive.Id)
	if err != nil {
		return nil, err
	}

	// Stream the archive into a staging key while hashing, then name it by
	// its hash as the runner does.
	prefix := samplesKeyPrefix + app.Uuid + "/" + version.Version + "/"
	staging := prefix + "staging-" + runId + ".zip"
	sha, bytes, err := s.copyArchive(ctx, link, staging)
	if err != nil {
		return nil, err
	}
	key := prefix + sha + ".zip"
	if _, err := s.S3.CopyObject(ctx, &s3.CopyObjectInput{
		Bucket: aws.String(s.Bucket), Key: aws.String(key),
		CopySource: aws.String(s.Bucket + "/" + staging), ContentType: aws.String("application/zip"), MetadataDirective: "REPLACE",
	}); err != nil {
		return nil, fmt.Errorf("naming the archive by its hash: %w", err)
	}
	_, _ = s.S3.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(s.Bucket), Key: aws.String(staging)})

	finished := run.CompletedAt
	if finished == "" {
		finished = s.Now().Format(time.RFC3339)
	}
	manifest := map[string]any{
		"version": 1, "status": "ready",
		"results":  map[string]any{"key": key, "bytes": bytes, "sha256": sha},
		"warnings": []string{}, "files": archive.FileCount,
		"startedAt": run.StartedAt, "finishedAt": finished, "buildId": "run:" + runId,
		"durationSeconds": durationSeconds(run.StartedAt, finished),
		"node":            map[string]any{"computeNodeId": run.ComputeNodeUuid, "source": sampleRunNodeSource},
	}
	doc, _ := json.Marshal(manifest)
	if err := s.S3.Upload(ctx, s.Bucket, prefix+"manifest.json", strings.NewReader(string(doc)), "application/json"); err != nil {
		return nil, fmt.Errorf("writing the sample manifest: %w", err)
	}

	registered := &SampleRegistered{
		Environment: s.Environment, ApplicationId: app.Uuid, VersionId: version.Uuid, Tag: version.Version,
		RunId: runId, ComputeNodeId: run.ComputeNodeUuid, Sha256: sha, Bytes: bytes, Files: archive.FileCount, Key: key,
		VersionsTable: s.VersionsTable,
	}
	detail, _ := json.Marshal(registered)
	out, err := s.Events.PutEvents(ctx, &eventbridge.PutEventsInput{Entries: []ebtypes.PutEventsRequestEntry{{
		Source: aws.String(sampleRegisteredSource), DetailType: aws.String(sampleRegisteredDetailType),
		Detail: aws.String(string(detail)), Resources: []string{version.Uuid},
	}}})
	if err == nil && out != nil && out.FailedEntryCount > 0 && len(out.Entries) > 0 {
		err = fmt.Errorf("%s: %s", aws.ToString(out.Entries[0].ErrorCode), aws.ToString(out.Entries[0].ErrorMessage))
	}
	if err != nil {
		return nil, fmt.Errorf("telling the status lambda: %w", err)
	}
	return registered, nil
}

func durationSeconds(started, finished string) int {
	a, err1 := time.Parse(time.RFC3339, started)
	b, err2 := time.Parse(time.RFC3339, finished)
	if err1 != nil || err2 != nil || b.Before(a) {
		return 0
	}
	return int(b.Sub(a).Seconds())
}

// readRun is the run as workflow-service shows its creator.
func (s *sampleRunService) readRun(ctx context.Context, authorization, runId string) (*workflowRun, error) {
	var run workflowRun
	status, err := s.getJson(ctx, authorization, s.WorkflowsApiUrl+"/runs/"+url.PathEscape(runId), &run)
	if err != nil {
		return nil, fmt.Errorf("reading the run from workflow-service: %w", err)
	}
	if status == http.StatusNotFound {
		return nil, refuse(http.StatusNotFound, "run_not_found", "no run "+runId+" of yours")
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("workflow-service answered %d for the run", status)
	}
	return &run, nil
}

// readArchive is the run's results archive, once download-service has it ready.
func (s *sampleRunService) readArchive(ctx context.Context, authorization, runId string) (*runArchive, error) {
	var listing struct {
		Downloads []runArchive `json:"downloads"`
	}
	status, err := s.getJson(ctx, authorization, s.DownloadsApiUrl+"/runs/archives", &listing)
	if err != nil {
		return nil, fmt.Errorf("listing run archives at download-service: %w", err)
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("download-service answered %d for the run archives", status)
	}
	var found *runArchive
	for i := range listing.Downloads {
		if listing.Downloads[i].RunId == runId {
			found = &listing.Downloads[i]
			break
		}
	}
	if found == nil {
		return nil, refuse(http.StatusConflict, "archive_missing", "the run has no results archive yet; it is made shortly after the run ends")
	}
	switch strings.ToUpper(found.Status) {
	case "READY":
		return found, nil
	case "FAILED":
		return nil, refuse(http.StatusConflict, "archive_failed", "the run's results archive could not be made")
	default:
		return nil, refuse(http.StatusConflict, "archive_not_ready", "the run's results archive is still being made; try again in a moment")
	}
}

func (s *sampleRunService) archiveLink(ctx context.Context, authorization, archiveId string) (string, error) {
	var out struct {
		Url string `json:"url"`
	}
	status, err := s.getJson(ctx, authorization, s.DownloadsApiUrl+"/runs/archives/"+url.PathEscape(archiveId)+"/url", &out)
	if err != nil {
		return "", fmt.Errorf("asking download-service for the archive link: %w", err)
	}
	if status != http.StatusOK || out.Url == "" {
		return "", fmt.Errorf("download-service answered %d for the archive link", status)
	}
	return out.Url, nil
}

func (s *sampleRunService) getJson(ctx context.Context, authorization, link string, into any) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, link, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", authorization)
	req.Header.Set("Accept", "application/json")
	resp, err := s.Http.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return resp.StatusCode, err
	}
	if resp.StatusCode == http.StatusOK {
		if err := json.Unmarshal(body, into); err != nil {
			return resp.StatusCode, fmt.Errorf("not JSON: %w", err)
		}
	}
	return resp.StatusCode, nil
}

// copyArchive streams the archive from its link into the staging key,
// returning its sha256 and size.
func (s *sampleRunService) copyArchive(ctx context.Context, link, staging string) (string, int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, link, nil)
	if err != nil {
		return "", 0, err
	}
	resp, err := s.Http.Do(req)
	if err != nil {
		return "", 0, fmt.Errorf("fetching the archive: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", 0, fmt.Errorf("the archive link answered %d", resp.StatusCode)
	}
	if resp.ContentLength > maxSampleArchiveBytes {
		return "", 0, refuse(http.StatusConflict, "archive_too_large", fmt.Sprintf("the results archive is %d bytes; a sample's results must stay under %d", resp.ContentLength, maxSampleArchiveBytes))
	}
	hash := sha256.New()
	counter := &countingReader{R: io.TeeReader(io.LimitReader(resp.Body, maxSampleArchiveBytes+1), hash)}
	if err := s.S3.Upload(ctx, s.Bucket, staging, counter, "application/zip"); err != nil {
		return "", 0, fmt.Errorf("copying the archive into the fixtures bucket: %w", err)
	}
	if counter.N > maxSampleArchiveBytes {
		_, _ = s.S3.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(s.Bucket), Key: aws.String(staging)})
		return "", 0, refuse(http.StatusConflict, "archive_too_large", "the results archive is larger than a sample's results may be")
	}
	return hex.EncodeToString(hash.Sum(nil)), counter.N, nil
}

type countingReader struct {
	R io.Reader
	N int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.R.Read(p)
	c.N += int64(n)
	return n, err
}

// --- handlers ----------------------------------------------------------------

func sampleRunRequest(ctx context.Context, request events.APIGatewayV2HTTPRequest) (*sampleRunService, *authorizer.Claims, string, string, *events.APIGatewayV2HTTPResponse) {
	appId, versionId := request.PathParameters["id"], request.PathParameters["versionId"]
	if appId == "" || versionId == "" {
		resp := sampleRunResponse(http.StatusBadRequest, map[string]any{"code": "bad_request", "message": "app id and version id are required"})
		return nil, nil, "", "", &resp
	}
	svc, err := sampleRunServiceFromEnv(ctx)
	if err != nil {
		resp := sampleRunResponse(http.StatusNotImplemented, map[string]any{"code": "no_sample_pipeline", "message": err.Error()})
		return nil, nil, "", "", &resp
	}
	claims := authorizer.ParseClaims(request.RequestContext.Authorizer.Lambda)
	return svc, claims, appId, versionId, nil
}

func sampleRunResponse(status int, body any) events.APIGatewayV2HTTPResponse {
	b, _ := json.Marshal(body)
	return events.APIGatewayV2HTTPResponse{StatusCode: status, Headers: map[string]string{"Content-Type": "application/json"}, Body: string(b)}
}

func sampleRunFailure(handlerName string, err error) events.APIGatewayV2HTTPResponse {
	var refusal *sampleRunError
	if errors.As(err, &refusal) {
		return sampleRunResponse(refusal.Status, map[string]any{"code": refusal.Code, "message": refusal.Message})
	}
	return events.APIGatewayV2HTTPResponse{StatusCode: http.StatusInternalServerError, Body: handlerError(handlerName, err)}
}

// GetSampleRunPlanHandler: GET /store/{id}/versions/{versionId}/sample-run/plan
func GetSampleRunPlanHandler(ctx context.Context, request events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	svc, claims, appId, versionId, early := sampleRunRequest(ctx, request)
	if early != nil {
		return *early, nil
	}
	plan, err := svc.Plan(ctx, claims, appId, versionId)
	if err != nil {
		return sampleRunFailure("GetSampleRunPlanHandler", err), nil
	}
	return sampleRunResponse(http.StatusOK, plan), nil
}

// PostSampleRunHandler: POST /store/{id}/versions/{versionId}/sample-run
func PostSampleRunHandler(ctx context.Context, request events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	svc, claims, appId, versionId, early := sampleRunRequest(ctx, request)
	if early != nil {
		return *early, nil
	}
	var body SampleRunStarted
	if err := json.Unmarshal([]byte(request.Body), &body); err != nil {
		return sampleRunResponse(http.StatusBadRequest, map[string]any{"code": "bad_request", "message": "the body must be JSON with runId and computeNodeId"}), nil
	}
	rec, err := svc.Started(ctx, claims, appId, versionId, body)
	if err != nil {
		return sampleRunFailure("PostSampleRunHandler", err), nil
	}
	return sampleRunResponse(http.StatusAccepted, map[string]any{"status": rec.Status, "runId": rec.RunId, "startedAt": rec.StartedAt}), nil
}

// PostSampleRunResultsHandler: POST /store/{id}/versions/{versionId}/sample-run/results
func PostSampleRunResultsHandler(ctx context.Context, request events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	svc, claims, appId, versionId, early := sampleRunRequest(ctx, request)
	if early != nil {
		return *early, nil
	}
	var body SampleRunResults
	if err := json.Unmarshal([]byte(request.Body), &body); err != nil {
		return sampleRunResponse(http.StatusBadRequest, map[string]any{"code": "bad_request", "message": "the body must be JSON with runId"}), nil
	}
	authorization := request.Headers["authorization"]
	if authorization == "" {
		authorization = request.Headers["Authorization"]
	}
	registered, err := svc.Results(ctx, claims, authorization, appId, versionId, body)
	if err != nil {
		return sampleRunFailure("PostSampleRunResultsHandler", err), nil
	}
	return sampleRunResponse(http.StatusAccepted, registered), nil
}

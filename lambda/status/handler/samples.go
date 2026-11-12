package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/expression"
	"github.com/aws/aws-sdk-go-v2/service/codebuild"
	cbtypes "github.com/aws/aws-sdk-go-v2/service/codebuild/types"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/pennsieve/app-deploy-service/status/models"
)

// Sample runs (docs/builds.md "Sample runs"). Once a release's fixture
// bundle is ready, the status lambda starts a build of the sample-runner
// project, which runs the promoted image once on the bundle with no network
// (image-builder/sample-run.sh) and leaves a results zip and a manifest in
// the fixtures bucket under samples/<applicationId>/<tag>/. Its state change
// comes back here: the manifest becomes the version's sampleRun record,
// older releases' results are tagged for expiry, and the release event goes
// out with fixtures and sampleRun. An app the runner cannot run (GPU only,
// or more memory than any of its compute types) is recorded needs_hardware
// and the event goes out at once: the owner's own-node fallback is for
// those.

// Environment variables (main.go).
const (
	SampleRunnerProjectEnvVar = "SAMPLE_RUNNER_PROJECT"
	SampleRunnerComputeEnvVar = "SAMPLE_RUNNER_COMPUTE"
)

// Variables a sample run gets (sample-run.sh reads IMAGE, BUNDLE_KEY,
// APPLICATION_ID, TAG, PARAMETERS, MEMORY_MB; VERSION_ID and
// APPLICATIONS_TABLE name the version to record on).
const (
	SampleImageVar      = "IMAGE"
	SampleBundleKeyVar  = "BUNDLE_KEY"
	SampleParametersVar = "PARAMETERS"
	SampleMemoryVar     = "MEMORY_MB"
)

// samplesKeyPrefix is where an app's results live (sample-run.sh).
const samplesKeyPrefix = "samples/"

// defaultSampleMemoryMiB is a container's memory when app.yml declares none.
const defaultSampleMemoryMiB = 2048

// ComputeTier is a CodeBuild compute type and the most memory (MiB) an app
// may declare to run on it.
type ComputeTier struct {
	Type      string
	MemoryMiB int
}

// ParseSampleCompute parses "BUILD_GENERAL1_MEDIUM=6144,BUILD_GENERAL1_LARGE=14336".
func ParseSampleCompute(spec string) []ComputeTier {
	var tiers []ComputeTier
	for _, part := range strings.Split(spec, ",") {
		name, limit, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			continue
		}
		n, err := strconv.Atoi(strings.TrimSpace(limit))
		if err != nil || n <= 0 || strings.TrimSpace(name) == "" {
			continue
		}
		tiers = append(tiers, ComputeTier{Type: strings.TrimSpace(name), MemoryMiB: n})
	}
	return tiers
}

// isSampleRun: a Build State Change event of the sample-runner project.
func (h *DeployTaskStateChangeHandler) isSampleRun(event models.BuildStateChangeEvent) bool {
	return h.Fixtures != nil && h.Fixtures.SampleProject != "" && event.Detail.ProjectName == h.Fixtures.SampleProject
}

// gpuOnly: the runtime's computeTypes list gpu and nothing else.
func gpuOnly(computeTypes []string) bool {
	if len(computeTypes) == 0 {
		return false
	}
	for _, t := range computeTypes {
		if !strings.EqualFold(strings.TrimSpace(t), "gpu") {
			return false
		}
	}
	return true
}

// computeFor picks the first tier whose memory covers the app's; "" when
// none does (or the memory is unknown and no tier exists).
func computeFor(tiers []ComputeTier, memoryMiB int) string {
	for _, t := range tiers {
		if memoryMiB <= t.MemoryMiB {
			return t.Type
		}
	}
	return ""
}

// imageByDigestOf is the version's image by the digest the build recorded:
// <registry>/<repo>@sha256:..., exactly what was promoted.
func imageByDigestOf(destination, digest string) string {
	if destination == "" || digest == "" {
		return ""
	}
	return imageByDigest(destination, digest)
}

// startSampleRun runs after the fixtures are ready. Best effort.
func (h *DeployTaskStateChangeHandler) startSampleRun(ctx context.Context, logger *slog.Logger, rel release, fixtures models.Fixtures, versionsTable string) {
	fb := h.Fixtures
	skip := func(code, message string) {
		logger.Info("sample: skipped", slog.String("reason", code))
		rec := models.SampleRun{Status: models.SampleSkipped, Error: &models.FixturesError{Code: code, Message: message}, UpdatedAt: fb.now()}
		h.setSampleRun(ctx, logger, rel, versionsTable, rec)
		h.emitReleaseReady(ctx, logger, rel, fixtures, &rec)
	}
	if fixtures.Status != models.FixturesReady || fixtures.Key == "" {
		skip(models.SampleErrorNoBundle, "the fixtures are not ready, so there is nothing to run the app on")
		return
	}
	if gpuOnly(rel.Runtime.ComputeTypes) {
		skip(models.SampleErrorNeedsHardware, "the app runs on GPU only; the sample runner has none")
		return
	}
	memory := rel.Runtime.Memory
	if memory <= 0 {
		memory = defaultSampleMemoryMiB
	}
	compute := computeFor(fb.SampleCompute, memory)
	if compute == "" {
		skip(models.SampleErrorNeedsHardware, fmt.Sprintf("the app declares %d MiB of memory, more than the sample runner has", memory))
		return
	}
	if base, err := h.reportedPatchBase(ctx, rel, versionsTable); err != nil {
		logger.Warn("sample: unable to check for a patch release; running it", slog.Any("error", err))
	} else if base != "" {
		skip(models.SampleErrorPatchRelease, fmt.Sprintf("a patch release: %s keeps the release report of %s; the owner can generate one from the version's row", rel.Tag, base))
		return
	}
	image := imageByDigestOf(rel.Destination, rel.ImageDigest)
	if image == "" {
		skip(models.SampleErrorBuildFailed, "the version has no promoted image to run")
		return
	}
	params, _ := json.Marshal(rel.Parameters)
	plain := func(name, value string) cbtypes.EnvironmentVariable {
		return cbtypes.EnvironmentVariable{Name: aws.String(name), Value: aws.String(value), Type: cbtypes.EnvironmentVariableTypePlaintext}
	}
	out, err := fb.CodeBuild.StartBuild(ctx, &codebuild.StartBuildInput{
		ProjectName:         aws.String(fb.SampleProject),
		ComputeTypeOverride: cbtypes.ComputeType(compute),
		EnvironmentVariablesOverride: []cbtypes.EnvironmentVariable{
			plain(SampleImageVar, image),
			plain(SampleBundleKeyVar, fixtures.Key),
			plain(FixtureApplicationIdVar, rel.ApplicationId),
			plain(FixtureTagVar, rel.Tag),
			plain(FixtureVersionIdVar, rel.VersionId),
			plain(BuildApplicationsTableVar, versionsTable),
			plain(SampleParametersVar, string(params)),
			plain(SampleMemoryVar, strconv.Itoa(memory)),
		},
	})
	if err != nil {
		logger.Warn("sample: unable to start the run", slog.Any("error", err))
		rec := models.SampleRun{Status: models.SampleFailed, Error: &models.FixturesError{Code: models.SampleErrorBuildFailed, Message: "the sample run could not be started: " + err.Error()}, UpdatedAt: fb.now()}
		h.setSampleRun(ctx, logger, rel, versionsTable, rec)
		h.emitReleaseReady(ctx, logger, rel, fixtures, &rec)
		return
	}
	buildId := ""
	if out != nil && out.Build != nil {
		buildId = aws.ToString(out.Build.Id)
	}
	logger.Info("sample: run started", slog.String("sampleBuildId", buildId), slog.String("compute", compute), slog.Int("memoryMiB", memory))
	h.setSampleRun(ctx, logger, rel, versionsTable, models.SampleRun{Status: models.SampleRunning, BuildId: buildId, ComputeType: compute, StartedAt: fb.now(), UpdatedAt: fb.now()})
}

// HandleSampleRun records how a sample run ended on its version and sends
// the release event. IN_PROGRESS is ignored.
func (h *DeployTaskStateChangeHandler) HandleSampleRun(ctx context.Context, event models.BuildStateChangeEvent) error {
	fb := h.Fixtures
	logger := h.logger.With(slog.String("sampleBuildId", event.Detail.BuildId), slog.String("buildStatus", event.Detail.BuildStatus))
	failed, final := buildFinalStates[event.Detail.BuildStatus]
	if !final {
		if event.Detail.BuildStatus != models.BuildInProgress {
			logger.Warn("ignoring sample run event with unknown status")
		}
		return nil
	}
	vars, err := h.fixtureBuildVariables(ctx, event)
	if err != nil {
		return err
	}
	versionId, versionsTable := vars[FixtureVersionIdVar], vars[BuildApplicationsTableVar]
	applicationId, tag := vars[FixtureApplicationIdVar], vars[FixtureTagVar]
	if versionId == "" || versionsTable == "" || applicationId == "" || tag == "" {
		return fmt.Errorf("sample run %s is missing its version variables", event.Detail.BuildId)
	}
	logger = logger.With(slog.String("versionId", versionId), slog.String("applicationId", applicationId), slog.String("tag", tag))

	rec := models.SampleRun{BuildId: event.Detail.BuildId, UpdatedAt: fb.now()}
	if failed {
		rec.Status = models.SampleFailed
		rec.Error = &models.FixturesError{
			Code:    "build_" + strings.ToLower(event.Detail.BuildStatus),
			Message: h.buildFailureReason(ctx, event, "sample run"),
		}
	} else if manifest, err := h.readSampleManifest(ctx, applicationId, tag); err != nil {
		logger.Warn("sample: the build succeeded but its manifest could not be read", slog.Any("error", err))
		rec.Status = models.SampleFailed
		rec.Error = &models.FixturesError{Code: models.SampleErrorManifestMissing, Message: err.Error()}
	} else {
		rec = sampleFromManifest(manifest, rec, fb.Bucket)
	}
	rel := release{VersionId: versionId, ApplicationId: applicationId, Tag: tag}
	if full, err := h.releaseFacts(ctx, versionId, versionsTable); err == nil {
		rel = full
		if rel.SampleRun != nil && rel.SampleRun.ComputeType != "" {
			rec.ComputeType = rel.SampleRun.ComputeType
		}
	} else {
		logger.Warn("sample: unable to read the release for its event; sending what is known", slog.Any("error", err))
	}
	h.setSampleRun(ctx, logger, rel, versionsTable, rec)
	if rec.Status == models.SampleReady {
		h.expireSuperseded(ctx, logger, samplesKeyPrefix+applicationId+"/", ".zip", tag)
	}
	fixtures := models.Fixtures{Status: models.FixturesReady}
	if rel.Fixtures != nil {
		fixtures = *rel.Fixtures
	}
	h.emitReleaseReady(ctx, logger, rel, fixtures, &rec)
	logger.Info("sample: recorded", slog.String("status", rec.Status))
	return nil
}

// HandleSampleRegistered takes up the service lambda's event for a sample
// run on a compute node of the owner's: its results archive and manifest
// are in the fixtures bucket under the runner's keys, so the version is
// recorded from the manifest as a runner's would be, and the release event
// goes out.
func (h *DeployTaskStateChangeHandler) HandleSampleRegistered(ctx context.Context, d models.SampleRegistered) error {
	if h.Fixtures == nil {
		return fmt.Errorf("sample registered for %s: this environment has no fixture builder", d.VersionId)
	}
	fb := h.Fixtures
	if d.VersionId == "" || d.ApplicationId == "" || d.Tag == "" || d.VersionsTable == "" {
		return fmt.Errorf("sample registered event is missing its version (%+v)", d)
	}
	logger := h.logger.With(slog.String("versionId", d.VersionId), slog.String("applicationId", d.ApplicationId),
		slog.String("tag", d.Tag), slog.String("runId", d.RunId))
	rec := models.SampleRun{BuildId: "run:" + d.RunId, UpdatedAt: fb.now(), RunId: d.RunId, ComputeNodeId: d.ComputeNodeId, Source: "node"}
	rel := release{VersionId: d.VersionId, ApplicationId: d.ApplicationId, Tag: d.Tag}
	if full, err := h.releaseFacts(ctx, d.VersionId, d.VersionsTable); err == nil {
		rel = full
		if rel.SampleRun != nil {
			rec.StartedBy = rel.SampleRun.StartedBy
			if rec.ComputeNodeId == "" {
				rec.ComputeNodeId = rel.SampleRun.ComputeNodeId
			}
		}
	} else {
		logger.Warn("sample: unable to read the release for its event; sending what is known", slog.Any("error", err))
	}
	if manifest, err := h.readSampleManifest(ctx, d.ApplicationId, d.Tag); err != nil {
		logger.Warn("sample: registered, but the manifest could not be read", slog.Any("error", err))
		rec.Status = models.SampleFailed
		rec.Error = &models.FixturesError{Code: models.SampleErrorManifestMissing, Message: err.Error()}
	} else {
		rec = sampleFromManifest(manifest, rec, fb.Bucket)
	}
	h.setSampleRun(ctx, logger, rel, d.VersionsTable, rec)
	if rec.Status == models.SampleReady {
		h.expireSuperseded(ctx, logger, samplesKeyPrefix+d.ApplicationId+"/", ".zip", d.Tag)
	}
	fixtures := models.Fixtures{Status: models.FixturesReady}
	if rel.Fixtures != nil {
		fixtures = *rel.Fixtures
	}
	h.emitReleaseReady(ctx, logger, rel, fixtures, &rec)
	logger.Info("sample: recorded from a compute node run", slog.String("status", rec.Status))
	return nil
}

// sampleFromManifest is the version's record of a finished sample run.
func sampleFromManifest(m models.SampleRunManifest, rec models.SampleRun, bucket string) models.SampleRun {
	rec.Warnings, rec.Files, rec.StartedAt, rec.FinishedAt = m.Warnings, m.Files, m.StartedAt, m.FinishedAt
	rec.ExitCode, rec.DurationSeconds = m.ExitCode, m.DurationSeconds
	if m.BuildId != "" {
		rec.BuildId = m.BuildId
	}
	if m.Node != nil {
		rec.ComputeNodeId, rec.Source = m.Node.ComputeNodeId, m.Node.Source
	}
	switch {
	case m.Status == models.SampleReady && m.Results != nil && m.Results.Key != "":
		rec.Status = models.SampleReady
		rec.Bucket, rec.Key, rec.Bytes, rec.Sha256 = bucket, m.Results.Key, m.Results.Bytes, m.Results.Sha256
	default:
		rec.Status = models.SampleFailed
		rec.Error = m.Error
		if rec.Error == nil {
			rec.Error = &models.FixturesError{Code: "internal", Message: "the sample run's manifest names no results and no error"}
		}
		if tail := strings.TrimSpace(m.LogTail); tail != "" && rec.Error.Message != "" {
			// the app's own last lines, for the owner
			if len(tail) > 1500 {
				tail = "…" + tail[len(tail)-1500:]
			}
			rec.Error.Message = rec.Error.Message + "\n" + tail
		}
	}
	return rec
}

// readSampleManifest reads samples/<applicationId>/<tag>/manifest.json.
func (h *DeployTaskStateChangeHandler) readSampleManifest(ctx context.Context, applicationId, tag string) (models.SampleRunManifest, error) {
	var m models.SampleRunManifest
	key := samplesKeyPrefix + applicationId + "/" + tag + "/manifest.json"
	out, err := h.Fixtures.S3.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(h.Fixtures.Bucket), Key: aws.String(key)})
	if err != nil {
		return m, fmt.Errorf("reading s3://%s/%s: %w", h.Fixtures.Bucket, key, err)
	}
	defer out.Body.Close()
	body, err := io.ReadAll(io.LimitReader(out.Body, 4<<20))
	if err != nil {
		return m, fmt.Errorf("reading s3://%s/%s: %w", h.Fixtures.Bucket, key, err)
	}
	if err := json.Unmarshal(body, &m); err != nil {
		return m, fmt.Errorf("s3://%s/%s is not a manifest: %w", h.Fixtures.Bucket, key, err)
	}
	return m, nil
}

// setSampleRun records the sampleRun attribute on the version and tells the
// version's channel. Best effort.
func (h *DeployTaskStateChangeHandler) setSampleRun(ctx context.Context, logger *slog.Logger, rel release, versionsTable string, rec models.SampleRun) {
	in, err := SampleRunUpdate(rel.VersionId, versionsTable, rec)
	if err == nil {
		_, err = h.DynamoDBApi.UpdateItem(ctx, in)
	}
	if err != nil {
		logger.Warn("sample: unable to record on the version", slog.String("status", rec.Status), slog.Any("error", err))
		return
	}
	h.SendSampleRunStatusEvent(ctx, rel.VersionId, rec)
}

// SampleRunUpdate is the update setSampleRun makes.
func SampleRunUpdate(versionId, versionsTable string, rec models.SampleRun) (*dynamodb.UpdateItemInput, error) {
	expressions, err := expression.NewBuilder().
		WithCondition(expression.AttributeExists(expression.Name(models.ApplicationKeyField))).
		WithUpdate(expression.Set(expression.Name(models.SampleRunField), expression.Value(rec))).
		Build()
	if err != nil {
		return nil, fmt.Errorf("error building the sample run update of %s: %w", versionId, err)
	}
	return &dynamodb.UpdateItemInput{
		Key:                       models.ApplicationKey(versionId),
		TableName:                 aws.String(versionsTable),
		ConditionExpression:       expressions.Condition(),
		ExpressionAttributeNames:  expressions.Names(),
		ExpressionAttributeValues: expressions.Values(),
		UpdateExpression:          expressions.Update(),
	}, nil
}

package models

// Sample runs (docs/builds.md "Sample runs"): what a version records about
// the one run of its image on its fixture bundle, under the sampleRun
// attribute, and the manifest sample-run.sh writes beside the results.

const SampleRunField = "sampleRun"

// Sample run statuses.
const (
	SampleRunning = "running" // the container is running (a build is in progress)
	SampleReady   = "ready"   // the results zip is in the bucket
	SampleSkipped = "skipped" // no run was started; Error says why
	SampleFailed  = "failed"  // the run or the build failed; Error says why
)

// Error codes set by the status lambda (sample-run.sh has its own: run_failed,
// timed_out, too_large, no_outputs, image_unavailable, bundle_unavailable,
// invalid_parameters).
const (
	SampleErrorNeedsHardware   = "needs_hardware" // GPU, or more memory than the runner has
	SampleErrorNoBundle        = "no_bundle"      // the fixtures are not ready
	SampleErrorBuildFailed     = "build_failed"
	SampleErrorManifestMissing = "manifest_missing"
)

// SampleRun is the version's record (and the release event's sampleRun).
type SampleRun struct {
	Status          string         `dynamodbav:"status" json:"status"`
	BuildId         string         `dynamodbav:"buildId,omitempty" json:"buildId,omitempty"`
	Bucket          string         `dynamodbav:"bucket,omitempty" json:"bucket,omitempty"`
	Key             string         `dynamodbav:"key,omitempty" json:"key,omitempty"`
	Bytes           int64          `dynamodbav:"bytes,omitempty" json:"bytes,omitempty"`
	Sha256          string         `dynamodbav:"sha256,omitempty" json:"sha256,omitempty"`
	Files           int            `dynamodbav:"files,omitempty" json:"files,omitempty"`
	ExitCode        *int           `dynamodbav:"exitCode,omitempty" json:"exitCode,omitempty"`
	DurationSeconds int            `dynamodbav:"durationSeconds,omitempty" json:"durationSeconds,omitempty"`
	ComputeType     string         `dynamodbav:"computeType,omitempty" json:"computeType,omitempty"`
	Warnings        []string       `dynamodbav:"warnings,omitempty" json:"warnings,omitempty"`
	Error           *FixturesError `dynamodbav:"error,omitempty" json:"error,omitempty"`
	StartedAt       string         `dynamodbav:"startedAt,omitempty" json:"startedAt,omitempty"`
	FinishedAt      string         `dynamodbav:"finishedAt,omitempty" json:"finishedAt,omitempty"`
	UpdatedAt       string         `dynamodbav:"updatedAt" json:"updatedAt"`
}

// SampleRunManifest is manifest.json as image-builder/sample-run.sh writes it.
type SampleRunManifest struct {
	Version         int            `json:"version"`
	Status          string         `json:"status"`
	Results         *SampleResults `json:"results,omitempty"`
	Error           *FixturesError `json:"error,omitempty"`
	Warnings        []string       `json:"warnings"`
	Files           int            `json:"files"`
	ExitCode        *int           `json:"exitCode"`
	TimedOut        bool           `json:"timedOut"`
	DurationSeconds int            `json:"durationSeconds"`
	StartedAt       string         `json:"startedAt"`
	FinishedAt      string         `json:"finishedAt"`
	BuildId         string         `json:"buildId"`
	LogTail         string         `json:"logTail"`
}

type SampleResults struct {
	Key    string `json:"key"`
	Bytes  int64  `json:"bytes"`
	Sha256 string `json:"sha256"`
}

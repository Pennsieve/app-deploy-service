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
	SampleErrorPatchRelease    = "patch_release"  // a patch of a release that already has a report
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
	// A sample run on a compute node of the owner's (the service lambda's
	// sample_run_node.go): the run, the node, who started it; Source "node".
	RunId         string `dynamodbav:"runId,omitempty" json:"runId,omitempty"`
	ComputeNodeId string `dynamodbav:"computeNodeId,omitempty" json:"computeNodeId,omitempty"`
	StartedBy     string `dynamodbav:"startedBy,omitempty" json:"startedBy,omitempty"`
	Source        string `dynamodbav:"source,omitempty" json:"source,omitempty"`
	// ReportOf is the earlier release in the same X.Y line whose release
	// report stands for this patch release, when this one gets none of its
	// own (skipped patch_release, or needs_hardware on a patch): readers
	// that look a report up by tag, such as Discover, show that one.
	ReportOf string `dynamodbav:"reportOf,omitempty" json:"reportOf,omitempty"`
}

// SampleRegisteredDetailType is the service lambda's event when an owner's
// sample run on a compute node has its results archive in the fixtures
// bucket (source ReleaseReadySource): the status lambda records the
// sample run from the manifest and emits App Release Ready.
const SampleRegisteredDetailType = "App Sample Run Registered"

// SampleRegistered is that event's detail.
type SampleRegistered struct {
	Environment   string `json:"environment"`
	ApplicationId string `json:"applicationId"`
	VersionId     string `json:"versionId"`
	Tag           string `json:"tag"`
	RunId         string `json:"runId"`
	ComputeNodeId string `json:"computeNodeId,omitempty"`
	VersionsTable string `json:"versionsTable"`
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
	// Node is set by a sample run on a compute node of the owner's.
	Node *SampleNode `json:"node,omitempty"`
}

type SampleNode struct {
	ComputeNodeId string `json:"computeNodeId"`
	Source        string `json:"source"`
}

type SampleResults struct {
	Key    string `json:"key"`
	Bytes  int64  `json:"bytes"`
	Sha256 string `json:"sha256"`
}

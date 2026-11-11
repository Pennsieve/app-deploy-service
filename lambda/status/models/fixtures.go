package models

// Fixture bundles (docs/builds.md "Fixture bundles"): what a version
// records about its review-block inputs built at release, under the
// fixtures attribute, and the manifest the fixture build writes beside the
// bundle.

const FixturesField = "fixtures"

// Fixture statuses.
const (
	FixturesBuilding = "building" // a fixture build is running
	FixturesReady    = "ready"    // the bundle is in the bucket
	FixturesSkipped  = "skipped"  // no build was started; Error says why
	FixturesFailed   = "failed"   // the build or the fixtures failed; Error says why
)

// Fixture error codes set by the status lambda (app-fixtures has its own,
// recorded as they come: no_review_block, dataset_too_large, ...).
const (
	FixturesErrorNoReviewBlock     = "no_review_block"
	FixturesErrorPrivateRepository = "private_repository"
	FixturesErrorPlatformApp       = "platform_app"
	FixturesErrorBuildFailed       = "build_failed"
	FixturesErrorManifestMissing   = "manifest_missing"
)

// Fixtures is the version's record (and the release event's fixtures).
type Fixtures struct {
	Status    string         `dynamodbav:"status" json:"status"`
	BuildId   string         `dynamodbav:"buildId,omitempty" json:"buildId,omitempty"`
	Bucket    string         `dynamodbav:"bucket,omitempty" json:"bucket,omitempty"`
	Key       string         `dynamodbav:"key,omitempty" json:"key,omitempty"`
	Bytes     int64          `dynamodbav:"bytes,omitempty" json:"bytes,omitempty"`
	Sha256    string         `dynamodbav:"sha256,omitempty" json:"sha256,omitempty"`
	Files     int            `dynamodbav:"files,omitempty" json:"files,omitempty"`
	Warnings  []string       `dynamodbav:"warnings,omitempty" json:"warnings,omitempty"`
	Error     *FixturesError `dynamodbav:"error,omitempty" json:"error,omitempty"`
	StartedAt string         `dynamodbav:"startedAt,omitempty" json:"startedAt,omitempty"`
	BuiltAt   string         `dynamodbav:"builtAt,omitempty" json:"builtAt,omitempty"`
	UpdatedAt string         `dynamodbav:"updatedAt" json:"updatedAt"`
}

type FixturesError struct {
	Code    string `dynamodbav:"code" json:"code"`
	Message string `dynamodbav:"message,omitempty" json:"message,omitempty"`
}

// FixturesManifest is manifest.json as image-builder/fixtures.sh writes it
// (the provenance crate is left in the bucket, not read here).
type FixturesManifest struct {
	Version   int            `json:"version"`
	Status    string         `json:"status"`
	Bundle    *FixtureBundle `json:"bundle,omitempty"`
	Warnings  []string       `json:"warnings"`
	Error     *FixturesError `json:"error,omitempty"`
	Files     int            `json:"files"`
	StartedAt string         `json:"startedAt"`
	BuiltAt   string         `json:"builtAt"`
	BuildId   string         `json:"buildId"`
}

type FixtureBundle struct {
	Key    string `json:"key"`
	Bytes  int64  `json:"bytes"`
	Sha256 string `json:"sha256"`
}

// ReleaseReadyDetailType and ReleaseReadySource name the EventBridge event
// the status lambda emits once per version when its image is deployed and
// its fixtures are decided (ready, skipped or failed).
const (
	ReleaseReadySource     = "pennsieve.app-deploy-service"
	ReleaseReadyDetailType = "App Release Ready"
)

// ReleaseReady is the event's detail.
type ReleaseReady struct {
	Environment   string   `json:"environment"`
	ApplicationId string   `json:"applicationId"`
	VersionId     string   `json:"versionId"`
	Tag           string   `json:"tag"`
	ImageDigest   string   `json:"imageDigest,omitempty"`
	SourceUrl     string   `json:"sourceUrl,omitempty"`
	Visibility    string   `json:"visibility,omitempty"`
	IsPrivate     bool     `json:"isPrivate"`
	Fixtures      Fixtures `json:"fixtures"`
}

package models

// The sample document (docs/builds.md "Sample runs", "The sample
// document"): what curation-service's App Sample Ready event says about a
// version's sample document, recorded under the sample attribute.

const SampleField = "sample"

// Sample statuses.
const (
	SampleDocReady  = "ready"  // the document is in the curation bucket
	SampleDocFailed = "failed" // the job failed; Error says why
)

// SampleReadySource and SampleReadyDetailType name the EventBridge event
// curation-service emits once per app-sample job that ended.
const (
	SampleReadySource     = "pennsieve.curation-service"
	SampleReadyDetailType = "App Sample Ready"
)

// SampleReadyDetail is the event's detail.
type SampleReadyDetail struct {
	Environment   string            `json:"environment"`
	ApplicationId string            `json:"applicationId"`
	VersionId     string            `json:"versionId"`
	Tag           string            `json:"tag"`
	JobId         string            `json:"jobId"`
	RunId         string            `json:"runId"`
	Status        string            `json:"status"` // ready | failed
	Bucket        string            `json:"bucket"`
	At            string            `json:"at"`
	Document      *SampleDocument   `json:"document,omitempty"`
	PreviewKey    string            `json:"previewKey,omitempty"`
	CrateKey      string            `json:"crateKey,omitempty"`
	Pages         int               `json:"pages,omitempty"`
	Figures       int               `json:"figures,omitempty"`
	Narrative     bool              `json:"narrative,omitempty"`
	Public        map[string]string `json:"public,omitempty"`
	Error         *FixturesError    `json:"error,omitempty"`
}

type SampleDocument struct {
	Key         string `json:"key"`
	Sha256      string `json:"sha256"`
	Bytes       int64  `json:"bytes"`
	Pages       int    `json:"pages"`
	ContentType string `json:"contentType"`
}

// Sample is the version's record of its sample document.
type Sample struct {
	Status      string            `dynamodbav:"status" json:"status"`
	JobId       string            `dynamodbav:"jobId,omitempty" json:"jobId,omitempty"`
	Bucket      string            `dynamodbav:"bucket,omitempty" json:"bucket,omitempty"`
	DocumentKey string            `dynamodbav:"documentKey,omitempty" json:"documentKey,omitempty"`
	PreviewKey  string            `dynamodbav:"previewKey,omitempty" json:"previewKey,omitempty"`
	CrateKey    string            `dynamodbav:"crateKey,omitempty" json:"crateKey,omitempty"`
	Sha256      string            `dynamodbav:"sha256,omitempty" json:"sha256,omitempty"`
	Bytes       int64             `dynamodbav:"bytes,omitempty" json:"bytes,omitempty"`
	Pages       int               `dynamodbav:"pages,omitempty" json:"pages,omitempty"`
	Figures     int               `dynamodbav:"figures,omitempty" json:"figures,omitempty"`
	Narrative   bool              `dynamodbav:"narrative,omitempty" json:"narrative,omitempty"`
	Public      map[string]string `dynamodbav:"public,omitempty" json:"public,omitempty"`
	Error       *FixturesError    `dynamodbav:"error,omitempty" json:"error,omitempty"`
	GeneratedAt string            `dynamodbav:"generatedAt,omitempty" json:"generatedAt,omitempty"`
	UpdatedAt   string            `dynamodbav:"updatedAt" json:"updatedAt"`
}

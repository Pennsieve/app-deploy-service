package events

import (
	"fmt"
	"time"
)

const ApplicationStatusEventName = "application_status_event"

// VariantStatusEventName is the status of a build of a variant other than
// the version's default, on the version's channel. It is not an
// application_status_event, which says the version's own status: a failed
// GPU build doesn't fail the version.
const VariantStatusEventName = "application_variant_status_event"

type ApplicationStatusEvent struct {
	ApplicationId string     `json:"application_id"`
	DeploymentId  string     `json:"deployment_id"`
	Status        string     `json:"status"`
	Time          *time.Time `json:"time,omitempty"`
	IsErrorStatus bool       `json:"is_error"`
	Source        string     `json:"source"`
	// Variant is the app variant the build is of; absent for an app
	// without variants.
	Variant string `json:"variant,omitempty"`
}

func ApplicationStatusChannel(applicationUuid string) string {
	return fmt.Sprintf("application-%s", applicationUuid)
}

// FixturesStatusEventName is a change of the version's fixture bundle
// (docs/builds.md "Fixture bundles"), on the version's channel: building,
// ready, skipped or failed. Not an application_status_event: fixtures
// never change the version's own status.
const FixturesStatusEventName = "application_fixtures_status_event"

type FixturesStatusEvent struct {
	ApplicationId string     `json:"application_id"`
	Status        string     `json:"status"`
	ErrorCode     string     `json:"error_code,omitempty"`
	ErrorMessage  string     `json:"error_message,omitempty"`
	Bytes         int64      `json:"bytes,omitempty"`
	Files         int        `json:"files,omitempty"`
	Time          *time.Time `json:"time,omitempty"`
	Source        string     `json:"source"`
}

// SampleRunStatusEventName is a change of the version's sample run (docs/
// builds.md "Sample runs"), on the version's channel: running, ready,
// skipped or failed.
const SampleRunStatusEventName = "application_sample_run_status_event"

type SampleRunStatusEvent struct {
	ApplicationId string     `json:"application_id"`
	Status        string     `json:"status"`
	ErrorCode     string     `json:"error_code,omitempty"`
	ErrorMessage  string     `json:"error_message,omitempty"`
	Bytes         int64      `json:"bytes,omitempty"`
	Files         int        `json:"files,omitempty"`
	Time          *time.Time `json:"time,omitempty"`
	Source        string     `json:"source"`
}

// SampleStatusEventName is a change of the version's sample document
// (docs/builds.md "The sample document"), on the version's channel: ready
// or failed.
const SampleStatusEventName = "application_sample_status_event"

type SampleStatusEvent struct {
	ApplicationId string     `json:"application_id"`
	Status        string     `json:"status"`
	DocumentKey   string     `json:"document_key,omitempty"`
	PreviewKey    string     `json:"preview_key,omitempty"`
	Pages         int        `json:"pages,omitempty"`
	ErrorCode     string     `json:"error_code,omitempty"`
	ErrorMessage  string     `json:"error_message,omitempty"`
	Time          *time.Time `json:"time,omitempty"`
	Source        string     `json:"source"`
}

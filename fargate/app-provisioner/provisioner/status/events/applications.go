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
	ApplicationId string    `json:"application_id"`
	DeploymentId  string    `json:"deployment_id"`
	Status        string    `json:"status"`
	Time          time.Time `json:"time"`
	IsErrorStatus bool      `json:"is_error"`
	Source        string    `json:"source"`
	// Variant is the app variant the build is of; absent for an app
	// without variants.
	Variant string `json:"variant,omitempty"`
}

func ApplicationStatusChannel(applicationUuid string) string {
	return fmt.Sprintf("application-%s", applicationUuid)
}

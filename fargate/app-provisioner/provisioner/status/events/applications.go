package events

import (
	"fmt"
	"time"
)

const ApplicationStatusEventName = "application_status_event"

type ApplicationStatusEvent struct {
	ApplicationId string    `json:"application_id"`
	DeploymentId  string    `json:"deployment_id"`
	Status        string    `json:"status"`
	Time          time.Time `json:"time"`
	IsErrorStatus bool      `json:"is_error"`
	Source        string    `json:"source"`
}

const ApplicationDeletionEventName = "application_deletion_event"

type ApplicationDeletionEvent struct {
	ApplicationId string    `json:"application_id"`
	Time          time.Time `json:"time"`
	Source        string    `json:"source"`
}

func ApplicationStatusChannel(applicationUuid string) string {
	return fmt.Sprintf("application-%s", applicationUuid)
}

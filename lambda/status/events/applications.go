package events

import (
	"fmt"
	"time"
)

const ApplicationStatusEventName = "application_status_event"

type ApplicationStatusEvent struct {
	ApplicationId string     `json:"application_id"`
	DeploymentId  string     `json:"deployment_id"`
	Status        string     `json:"status"`
	Time          *time.Time `json:"time,omitempty"`
	IsErrorStatus bool       `json:"is_error"`
	Source        string     `json:"source"`
}

func ApplicationStatusChannel(applicationUuid string) string {
	return fmt.Sprintf("application-%s", applicationUuid)
}

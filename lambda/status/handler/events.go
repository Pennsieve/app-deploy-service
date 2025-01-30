package handler

import (
	"github.com/pennsieve/app-deploy-service/status/events"
	"github.com/pennsieve/app-deploy-service/status/logging"
	"log"
	"log/slog"
	"time"
)

func (h *DeployTaskStateChangeHandler) SendApplicationStatusEvent(applicationId, deploymentId string, status string, isErrorStatus bool) {
	if h.PusherClient == nil {
		log.Printf("warning: no Pusher client configured")
		return
	}
	channel := events.ApplicationStatusChannel(applicationId)
	event := events.ApplicationStatusEvent{
		ApplicationId: applicationId,
		DeploymentId:  deploymentId,
		Status:        status,
		Time:          time.Now().UTC(),
		IsErrorStatus: isErrorStatus,
		Source:        "DeployTaskStateChangeHandler",
	}
	if err := h.PusherClient.Trigger(channel, events.ApplicationStatusEventName, event); err != nil {
		logging.Default.Warn("error updating pusher application channel",
			slog.String("channel", channel),
			slog.String("status", status),
			slog.Any("error", err))
	}
}

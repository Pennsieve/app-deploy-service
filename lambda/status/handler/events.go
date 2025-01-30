package handler

import (
	"github.com/pennsieve/app-deploy-service/status/events"
	"log"
	"log/slog"
	"time"
)

func (h *DeployTaskStateChangeHandler) SendApplicationStatusEvent(applicationId, deploymentId string, final *FinalState, updateTime *time.Time, logger *slog.Logger) {
	if h.PusherClient == nil {
		log.Printf("warning: no Pusher client configured")
		return
	}
	channel := events.ApplicationStatusChannel(applicationId)
	status := final.Status()
	isErrorStatus := final.Errored
	event := events.ApplicationStatusEvent{
		ApplicationId: applicationId,
		DeploymentId:  deploymentId,
		Status:        status,
		Time:          updateTime,
		IsErrorStatus: isErrorStatus,
		Source:        "DeployTaskStateChangeHandler",
	}
	if err := h.PusherClient.Trigger(channel, events.ApplicationStatusEventName, event); err != nil {
		logger.Warn("error updating pusher application channel",
			slog.String("channel", channel),
			slog.String("status", status),
			slog.Any("error", err))
	}
}

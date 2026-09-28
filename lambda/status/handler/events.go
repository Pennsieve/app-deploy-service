package handler

import (
	"context"

	"github.com/pennsieve/app-deploy-service/status/events"
	"github.com/pennsieve/pennsieve-go-core/pkg/realtime"
	"log"
	"log/slog"
	"time"
)

func (h *DeployTaskStateChangeHandler) SendApplicationStatusEvent(ctx context.Context, applicationId, deploymentId string, final *FinalState, updateTime *time.Time) {
	if h.Realtime == nil && h.PusherClient == nil {
		log.Printf("warning: no Pusher client configured")
		return
	}
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
	if h.Realtime != nil {
		channel := realtime.Application(applicationId)
		if err := h.Realtime.Publish(ctx, channel, events.ApplicationStatusEventName, event); err != nil {
			h.logger.Warn("error publishing realtime application channel",
				slog.String("channel", channel.Path()),
				slog.String("status", status),
				slog.Any("error", err))
		}
		return
	}
	channel := events.ApplicationStatusChannel(applicationId)
	if err := h.PusherClient.Trigger(channel, events.ApplicationStatusEventName, event); err != nil {
		h.logger.Warn("error updating pusher application channel",
			slog.String("channel", channel),
			slog.String("status", status),
			slog.Any("error", err))
	}
}

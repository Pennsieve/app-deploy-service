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
	h.publish(ctx, applicationId, events.ApplicationStatusEventName, h.statusEvent(applicationId, deploymentId, final, updateTime))
}

// SendVariantStatusEvent says how a variant's build ended. The default
// variant's status is the version's (an application_status_event naming the
// variant); another's goes out as its own event (VariantStatusEventName).
func (h *DeployTaskStateChangeHandler) SendVariantStatusEvent(ctx context.Context, applicationId, deploymentId, variant string, isDefault bool, final *FinalState, updateTime *time.Time) {
	event := h.statusEvent(applicationId, deploymentId, final, updateTime)
	event.Variant = variant
	name := events.VariantStatusEventName
	if isDefault {
		name = events.ApplicationStatusEventName
	}
	h.publish(ctx, applicationId, name, event)
}

func (h *DeployTaskStateChangeHandler) statusEvent(applicationId, deploymentId string, final *FinalState, updateTime *time.Time) events.ApplicationStatusEvent {
	return events.ApplicationStatusEvent{
		ApplicationId: applicationId,
		DeploymentId:  deploymentId,
		Status:        final.Status(),
		Time:          updateTime,
		IsErrorStatus: final.Errored,
		Source:        "DeployTaskStateChangeHandler",
	}
}

func (h *DeployTaskStateChangeHandler) publish(ctx context.Context, applicationId, name string, event events.ApplicationStatusEvent) {
	if h.Realtime == nil && h.PusherClient == nil {
		log.Printf("warning: no Pusher client configured")
		return
	}
	if h.Realtime != nil {
		channel := realtime.Application(applicationId)
		if err := h.Realtime.Publish(ctx, channel, name, event); err != nil {
			h.logger.Warn("error publishing realtime application channel",
				slog.String("channel", channel.Path()),
				slog.String("status", event.Status),
				slog.Any("error", err))
		}
		return
	}
	channel := events.ApplicationStatusChannel(applicationId)
	if err := h.PusherClient.Trigger(channel, name, event); err != nil {
		h.logger.Warn("error updating pusher application channel",
			slog.String("channel", channel),
			slog.String("status", event.Status),
			slog.Any("error", err))
	}
}

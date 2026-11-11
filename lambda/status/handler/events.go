package handler

import (
	"context"

	"github.com/pennsieve/app-deploy-service/status/events"
	"github.com/pennsieve/app-deploy-service/status/models"
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

// SendFixturesStatusEvent says how the version's fixture bundle stands
// (fixtures.go), so the app page follows it live like the build.
func (h *DeployTaskStateChangeHandler) SendFixturesStatusEvent(ctx context.Context, versionId string, rec models.Fixtures) {
	event := events.FixturesStatusEvent{
		ApplicationId: versionId,
		Status:        rec.Status,
		Bytes:         rec.Bytes,
		Files:         rec.Files,
		Source:        "DeployTaskStateChangeHandler",
	}
	if rec.Error != nil {
		event.ErrorCode, event.ErrorMessage = rec.Error.Code, rec.Error.Message
	}
	if t, err := time.Parse(time.RFC3339, rec.UpdatedAt); err == nil {
		event.Time = &t
	}
	h.publish(ctx, versionId, events.FixturesStatusEventName, event)
}

// publish sends an event on the version's channel (realtime, or Pusher
// where still configured). status is for the log.
func (h *DeployTaskStateChangeHandler) publish(ctx context.Context, applicationId, name string, event any) {
	if h.Realtime == nil && h.PusherClient == nil {
		log.Printf("warning: no Pusher client configured")
		return
	}
	if h.Realtime != nil {
		channel := realtime.Application(applicationId)
		if err := h.Realtime.Publish(ctx, channel, name, event); err != nil {
			h.logger.Warn("error publishing realtime application channel",
				slog.String("channel", channel.Path()),
				slog.String("event", name),
				slog.Any("error", err))
		}
		return
	}
	channel := events.ApplicationStatusChannel(applicationId)
	if err := h.PusherClient.Trigger(channel, name, event); err != nil {
		h.logger.Warn("error updating pusher application channel",
			slog.String("channel", channel),
			slog.String("event", name),
			slog.Any("error", err))
	}
}

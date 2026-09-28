package handler

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/pennsieve/app-deploy-service/status/events"
	"github.com/pennsieve/app-deploy-service/status/logging"
	"github.com/pennsieve/pennsieve-go-core/pkg/realtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type recordingPublisher struct {
	realtime.Noop
	channels []string
	names    []string
	data     []any
	err      error
}

func (p *recordingPublisher) Publish(_ context.Context, ch realtime.Channel, name string, data any) error {
	p.channels = append(p.channels, ch.Path())
	p.names = append(p.names, name)
	p.data = append(p.data, data)
	return p.err
}

func TestSendApplicationStatusEventToRealtime(t *testing.T) {
	publisher := &recordingPublisher{}
	h := (&DeployTaskStateChangeHandler{logger: logging.Default}).WithRealtime(publisher)
	updated := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)

	h.SendApplicationStatusEvent(context.Background(), "0f3c8a52-6a47-4a8e-9b1a-2d6b8f0c1e11", "dep-1", &FinalState{Errored: true}, &updated)

	require.Len(t, publisher.channels, 1)
	assert.Equal(t, "/applications/0f3c8a52-6a47-4a8e-9b1a-2d6b8f0c1e11", publisher.channels[0])
	assert.Equal(t, events.ApplicationStatusEventName, publisher.names[0])
	assert.Equal(t, events.ApplicationStatusEvent{
		ApplicationId: "0f3c8a52-6a47-4a8e-9b1a-2d6b8f0c1e11",
		DeploymentId:  "dep-1",
		Status:        "error",
		Time:          &updated,
		IsErrorStatus: true,
		Source:        "DeployTaskStateChangeHandler",
	}, publisher.data[0])
}

func TestSendApplicationStatusEventIgnoresRealtimeErrors(t *testing.T) {
	publisher := &recordingPublisher{err: errors.New("unauthorized")}
	h := (&DeployTaskStateChangeHandler{logger: logging.Default}).WithRealtime(publisher)

	h.SendApplicationStatusEvent(context.Background(), "app-1", "dep-1", &FinalState{}, nil)

	assert.Len(t, publisher.channels, 1)
}

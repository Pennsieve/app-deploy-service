package handler

import (
	"context"
	"errors"
	"testing"

	"github.com/pennsieve/app-deploy-service/service/events"
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

type fakeStatusStore struct{ statuses []string }

func (s *fakeStatusStore) UpdateStatus(_ context.Context, newStatus string, _ string) error {
	s.statuses = append(s.statuses, newStatus)
	return nil
}

func TestStatusManagerPublishesToRealtime(t *testing.T) {
	publisher := &recordingPublisher{}
	store := &fakeStatusStore{}
	m := NewAppStoreStatusManager("TestHandler", store, "0f3c8a52-6a47-4a8e-9b1a-2d6b8f0c1e11").WithRealtime(publisher)
	m.DeploymentId = "dep-1"

	m.UpdateApplicationStatus(context.Background(), m.ApplicationId, "pending")
	m.SetErrorStatus(context.Background(), errors.New("boom"))

	assert.Equal(t, []string{"pending", "error: boom"}, store.statuses)
	require.Len(t, publisher.channels, 2)
	assert.Equal(t, "/applications/0f3c8a52-6a47-4a8e-9b1a-2d6b8f0c1e11", publisher.channels[0])
	assert.Equal(t, []string{events.ApplicationStatusEventName, events.ApplicationStatusEventName}, publisher.names)

	first := publisher.data[0].(events.ApplicationStatusEvent)
	assert.Equal(t, "pending", first.Status)
	assert.False(t, first.IsErrorStatus)
	assert.Equal(t, "dep-1", first.DeploymentId)
	assert.Equal(t, "TestHandler", first.Source)
	second := publisher.data[1].(events.ApplicationStatusEvent)
	assert.Equal(t, "error: boom", second.Status)
	assert.True(t, second.IsErrorStatus)
}

func TestStatusManagerIgnoresRealtimeErrors(t *testing.T) {
	publisher := &recordingPublisher{err: errors.New("unauthorized")}
	store := &fakeStatusStore{}
	m := NewAppStoreStatusManager("TestHandler", store, "app-1").WithRealtime(publisher)

	m.UpdateApplicationStatus(context.Background(), "app-1", "deployed")

	assert.Equal(t, []string{"deployed"}, store.statuses)
	assert.Len(t, publisher.channels, 1)
}

func TestRealtimeEnabled(t *testing.T) {
	t.Setenv(realtime.EnvEventsEndpoint, "")
	assert.False(t, realtimeEnabled())
	t.Setenv(realtime.EnvEventsEndpoint, "  ")
	assert.False(t, realtimeEnabled())
	t.Setenv(realtime.EnvEventsEndpoint, "abc.appsync-api.us-east-1.amazonaws.com")
	assert.True(t, realtimeEnabled())
}

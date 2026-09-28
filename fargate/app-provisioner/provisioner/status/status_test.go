package status

import (
	"context"
	"errors"
	"testing"

	"github.com/pennsieve/app-deploy-service/app-provisioner/provisioner/status/events"
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

func TestManagerPublishesToRealtime(t *testing.T) {
	publisher := &recordingPublisher{}
	store := &fakeStatusStore{}
	m := NewAppStoreManager(store, "0f3c8a52-6a47-4a8e-9b1a-2d6b8f0c1e11").WithRealtime(publisher)

	m.UpdateApplicationStatus(context.Background(), "building", false)
	m.SetErrorStatus(context.Background(), errors.New("boom"))

	assert.Equal(t, []string{"building", "error: boom"}, store.statuses)
	require.Len(t, publisher.channels, 2)
	assert.Equal(t, "/applications/0f3c8a52-6a47-4a8e-9b1a-2d6b8f0c1e11", publisher.channels[0])
	assert.Equal(t, events.ApplicationStatusEventName, publisher.names[0])
	first := publisher.data[0].(events.ApplicationStatusEvent)
	assert.Equal(t, "building", first.Status)
	assert.Equal(t, "AppProvisioner", first.Source)
	assert.True(t, publisher.data[1].(events.ApplicationStatusEvent).IsErrorStatus)
}

func TestManagerIgnoresRealtimeErrors(t *testing.T) {
	publisher := &recordingPublisher{err: errors.New("unauthorized")}
	store := &fakeStatusStore{}
	m := NewAppStoreManager(store, "app-1").WithRealtime(publisher)

	m.UpdateApplicationStatus(context.Background(), "deployed", false)

	assert.Equal(t, []string{"deployed"}, store.statuses)
	assert.Len(t, publisher.channels, 1)
}

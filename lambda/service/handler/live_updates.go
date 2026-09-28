package handler

import (
	"context"
	"log"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/pennsieve/pennsieve-go-core/pkg/realtime"
	"github.com/pusher/pusher-http-go/v5"
)

// realtimeEnabled reports whether this environment publishes live updates through AppSync Events
// (REALTIME_EVENTS_ENDPOINT set) instead of Pusher.
func realtimeEnabled() bool {
	return strings.TrimSpace(os.Getenv(realtime.EnvEventsEndpoint)) != ""
}

// withLiveUpdates attaches the AppSync publisher when REALTIME_EVENTS_ENDPOINT is set, and otherwise the
// Pusher client from SSM. Neither failing stops the caller.
func withLiveUpdates(ctx context.Context, m *StatusManager, cfg aws.Config) *StatusManager {
	if realtimeEnabled() {
		return m.WithRealtime(realtime.FromEnv(ctx))
	}
	pusherConfig, err := GetPusherConfig(ctx, ssm.NewFromConfig(cfg))
	if err != nil {
		log.Printf("warning: %v\n", err)
		return m
	}
	return m.WithPusher(&pusher.Client{
		AppID:   pusherConfig.AppId,
		Key:     pusherConfig.Key,
		Secret:  pusherConfig.Secret,
		Cluster: pusherConfig.Cluster,
		Secure:  true,
	})
}

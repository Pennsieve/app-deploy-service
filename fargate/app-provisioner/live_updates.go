package main

import (
	"context"
	"log"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/pennsieve/app-deploy-service/app-provisioner/provisioner/pusher_config"
	"github.com/pennsieve/app-deploy-service/app-provisioner/provisioner/status"
	"github.com/pennsieve/pennsieve-go-core/pkg/realtime"
)

// withLiveUpdates attaches the AppSync publisher when REALTIME_EVENTS_ENDPOINT is set, and otherwise the
// Pusher client from SSM. Neither failing stops the provisioner.
func withLiveUpdates(ctx context.Context, m *status.Manager, cfg aws.Config) *status.Manager {
	if strings.TrimSpace(os.Getenv(realtime.EnvEventsEndpoint)) != "" {
		return m.WithRealtime(realtime.FromEnv(ctx))
	}
	pusherConfig, err := pusher_config.Get(ctx, ssm.NewFromConfig(cfg))
	if err != nil {
		log.Printf("warning: unable to configure Pusher: %s\n", err.Error())
		return m
	}
	return m.WithPusher(pusherConfig)
}

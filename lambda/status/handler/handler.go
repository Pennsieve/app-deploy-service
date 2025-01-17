package handler

import (
	"context"
	"fmt"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	"github.com/pennsieve/app-deploy-service/status/logging"
	"github.com/pennsieve/app-deploy-service/status/models"
	"log/slog"
)

var logger = logging.Default

func DeployStateChangeHandler(ctx context.Context, event models.TaskStateChangeEvent) error {
	logger.Info("handling event", slog.Any("event", event))
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return fmt.Errorf("error loading AWS config: %w", err)
	}
	ecsClient := ecs.NewFromConfig(cfg)
	ecsClient.DescribeTasks(ctx, &ecs.DescribeTasksInput{
		Tasks:   nil,
		Cluster: nil,
		Include: nil,
	})

	return nil
}

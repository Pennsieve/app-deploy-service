package handler

import (
	"context"
	"github.com/pennsieve/app-deploy-service/status/logging"
	"github.com/pennsieve/app-deploy-service/status/models"
	"log/slog"
)

var logger = logging.Default

func DeployStateChangeHandler(ctx context.Context, event models.TaskStateChangeEvent) error {
	logger.Info("handling event", slog.Any("event", event))

	return nil
}

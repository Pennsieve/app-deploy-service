package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/pennsieve/app-deploy-service/status/logging"
	"log/slog"
)

var logger = logging.Default

func DeployStateChangeHandler(ctx context.Context, event json.RawMessage) error {
	var eventMap map[string]any
	if err := json.Unmarshal(event, &eventMap); err != nil {
		return fmt.Errorf("error unmarshalling event %s: %w", string(event), err)
	}
	logger.Info("handling event", slog.Any("event", eventMap))

	return nil
}

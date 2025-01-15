package handler

import (
	"context"
	"encoding/json"
	"github.com/pennsieve/app-deploy-service/status/logging"
)

var logger = logging.Default

const ApplicationUUIDTag = "ApplicationUUID"
const ActionTag = "Action"

func DeployStateChangeHandler(ctx context.Context, event json.RawMessage) error {
	logger.Info(string(event))

	return nil
}

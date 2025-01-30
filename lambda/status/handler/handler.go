package handler

import (
	"context"
	"errors"
	"fmt"
	"github.com/pennsieve/app-deploy-service/status/external"
	"github.com/pennsieve/app-deploy-service/status/logging"
	"github.com/pennsieve/app-deploy-service/status/models"
	"github.com/pusher/pusher-http-go/v5"
	"log/slog"
)

type DeployTaskStateChangeHandler struct {
	ECSApi            external.ECSApi
	DynamoDBApi       external.DynamoDBApi
	PusherClient      *pusher.Client
	ApplicationsTable string
	DeploymentsTable  string
}

func NewDeployTaskStateChangeHandler(ecsApi external.ECSApi, dynamoDBApi external.DynamoDBApi, applicationsTable string, deploymentsTable string) *DeployTaskStateChangeHandler {
	return &DeployTaskStateChangeHandler{ECSApi: ecsApi, DynamoDBApi: dynamoDBApi, ApplicationsTable: applicationsTable, DeploymentsTable: deploymentsTable}
}

func (h *DeployTaskStateChangeHandler) WithPusher(pusherClient *pusher.Client) *DeployTaskStateChangeHandler {
	h.PusherClient = pusherClient
	return h
}

func (h *DeployTaskStateChangeHandler) Handle(ctx context.Context, event models.TaskStateChangeEvent) error {
	taskArn := event.Detail.TaskArn
	logger := logging.Default.With(slog.String("taskArn", taskArn))
	logger.Info("handling event", slog.Any("event", event))

	ids, err := h.GetIdsFromTags(ctx, taskArn, event.Detail.ClusterArn)
	if err != nil {
		return fmt.Errorf("error getting ids from task tags: %w", err)
	}

	deploymentId := ids.DeploymentId
	applicationId := ids.ApplicationId
	logger = logger.With(
		slog.String("deploymentId", deploymentId),
		slog.String("applicationId", applicationId))

	if err := h.UpdateDeploymentsTable(ctx, applicationId, deploymentId, event); err != nil {
		var conflict *DeploymentUpdateConflict
		if errors.As(err, &conflict) {
			if unmarshalError := conflict.UnmarshallingError; unmarshalError != nil {
				logger.Warn("ignoring event since more recent one exists; but there was an error unmarshalling existing deployment",
					slog.Any("error", unmarshalError))
			} else {
				logger.Error("ignoring event since more recent one exists",
					slog.Int("ignoredEventVersion", event.Detail.Version),
					slog.String("ignoredEventLastStatus", event.Detail.LastStatus),
					slog.Int("existingEventVersion", conflict.Existing.Version),
					slog.String("existingEventLastStatus", conflict.Existing.LastStatus))
			}
		}
		return err
	}

	if final := IsFinalState(event); final != nil {
		if err := h.UpdateApplicationsTable(ctx, applicationId, final); err != nil {
			return err
		}
	}

	return nil
}

type FinalState struct {
	Errored bool
}

func (f *FinalState) Status() string {
	if f.Errored {
		return "error"
	}
	return "deployed"
}

func IsFinalState(event models.TaskStateChangeEvent) *FinalState {
	if event.Detail.LastStatus != models.StateStopped {
		return nil
	}
	return &FinalState{Errored: event.Detail.Errored()}
}

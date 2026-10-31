package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/pennsieve/app-deploy-service/status/external"
	"github.com/pennsieve/app-deploy-service/status/logging"
	"github.com/pennsieve/app-deploy-service/status/models"
	"github.com/pennsieve/pennsieve-go-core/pkg/realtime"
	"github.com/pusher/pusher-http-go/v5"
	"log/slog"
)

type DeployTaskStateChangeHandler struct {
	ECSApi            external.ECSApi
	DynamoDBApi       external.DynamoDBApi
	PusherClient      *pusher.Client
	Realtime          realtime.Publisher
	ApplicationsTable string
	DeploymentsTable  string
	logger            *slog.Logger

	// CodeBuild image builds (handler/codebuild.go); nil where unused.
	CodeBuildApi external.CodeBuildApi
	LogsApi      external.LogsApi
	SSMApi       external.SSMApi

	// ECRApi reads the digest of an App Store build's image; nil where
	// unused (then no digest is recorded).
	ECRApi external.ECRApi
}

// WithECR lets the handler record the digest of each App Store image it sees
// built.
func (h *DeployTaskStateChangeHandler) WithECR(ecrApi external.ECRApi) *DeployTaskStateChangeHandler {
	h.ECRApi = ecrApi
	return h
}

// WithCodeBuild lets the handler follow CodeBuild image builds: look up a
// build's variables, read a failed build's log and delete its token parameter.
func (h *DeployTaskStateChangeHandler) WithCodeBuild(codeBuildApi external.CodeBuildApi, logsApi external.LogsApi, ssmApi external.SSMApi) *DeployTaskStateChangeHandler {
	h.CodeBuildApi = codeBuildApi
	h.LogsApi = logsApi
	h.SSMApi = ssmApi
	return h
}

func NewDeployTaskStateChangeHandler(ecsApi external.ECSApi, dynamoDBApi external.DynamoDBApi, applicationsTable string, deploymentsTable string) *DeployTaskStateChangeHandler {
	return &DeployTaskStateChangeHandler{ECSApi: ecsApi, DynamoDBApi: dynamoDBApi, ApplicationsTable: applicationsTable, DeploymentsTable: deploymentsTable, logger: logging.Default}
}

func (h *DeployTaskStateChangeHandler) WithPusher(pusherClient *pusher.Client) *DeployTaskStateChangeHandler {
	h.PusherClient = pusherClient
	return h
}

// WithRealtime publishes status events through AppSync Events instead of Pusher.
func (h *DeployTaskStateChangeHandler) WithRealtime(publisher realtime.Publisher) *DeployTaskStateChangeHandler {
	h.Realtime = publisher
	return h
}

// HandleEvent is the Lambda entry point. It takes ECS "Task State Change"
// events from the kaniko task and CodeBuild "Build State Change" events from
// the image-builder project, whichever builder the environment uses.
func (h *DeployTaskStateChangeHandler) HandleEvent(ctx context.Context, raw json.RawMessage) error {
	var envelope struct {
		Source string `json:"source"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return fmt.Errorf("error reading event: %w", err)
	}
	if envelope.Source == models.CodeBuildSource {
		var event models.BuildStateChangeEvent
		if err := json.Unmarshal(raw, &event); err != nil {
			return fmt.Errorf("error reading CodeBuild event: %w", err)
		}
		return h.HandleBuild(ctx, event)
	}
	var event models.TaskStateChangeEvent
	if err := json.Unmarshal(raw, &event); err != nil {
		return fmt.Errorf("error reading ECS event: %w", err)
	}
	return h.Handle(ctx, event)
}

func (h *DeployTaskStateChangeHandler) Handle(ctx context.Context, event models.TaskStateChangeEvent) error {
	taskArn := event.Detail.TaskArn
	h.logger = h.logger.With(slog.String("taskArn", taskArn))
	h.logger.Info("handling event", slog.Any("event", event))

	ids, err := h.GetIdsFromTags(ctx, taskArn, event.Detail.ClusterArn)
	if err != nil {
		return fmt.Errorf("error getting ids from task tags: %w", err)
	}
	return h.recordStateChange(ctx, ids, event)
}

// recordStateChange updates the deployment, and on a final state the
// application (or App Store version) and its live status. ECS task events
// (kaniko) and CodeBuild build events (handler/codebuild.go) both end here.
func (h *DeployTaskStateChangeHandler) recordStateChange(ctx context.Context, ids DeploymentApplicationIds, event models.TaskStateChangeEvent) error {
	deploymentId := ids.DeploymentId
	applicationId := ids.ApplicationId
	applicationsTable := h.ApplicationsTable
	if ids.ApplicationsTable != "" {
		applicationsTable = ids.ApplicationsTable
	}
	h.logger = h.logger.With(
		slog.String("deploymentId", deploymentId),
		slog.String("applicationId", applicationId),
		slog.String("applicationsTable", applicationsTable))

	if err := h.UpdateDeploymentsTable(ctx, applicationId, deploymentId, event); err != nil {
		var conflict *DeploymentUpdateConflict
		if errors.As(err, &conflict) {
			if unmarshalError := conflict.UnmarshallingError; unmarshalError != nil {
				h.logger.Warn("ignoring event since more recent one exists; but there was an error unmarshalling existing deployment",
					slog.Any("error", unmarshalError))
			} else {
				h.logger.Error("ignoring event since more recent one exists",
					slog.Int("ignoredEventVersion", event.Detail.Version),
					slog.String("ignoredEventLastStatus", event.Detail.LastStatus),
					slog.Int("existingEventVersion", conflict.Existing.Version),
					slog.String("existingEventLastStatus", conflict.Existing.LastStatus))
			}
		}
		return err
	}

	final := IsFinalState(event)
	if final == nil {
		return nil
	}
	// The digest of the image an App Store build pushed, before the status
	// says deployed, so a deployed version names the exact image. Best
	// effort: it never fails the event.
	if !final.Errored && h.isAppStoreBuild(ids) {
		h.recordImageDigest(ctx, ids, applicationsTable)
	}
	if ids.Variant == "" {
		// an app without variants (or not an App Store build): as always
		h.SendApplicationStatusEvent(ctx, applicationId, deploymentId, final, event.Detail.UpdatedAt)
		return h.UpdateApplicationsTable(ctx, applicationId, final, applicationsTable)
	}

	// A variant's build: its own status and reason on the version's
	// variants. The default variant's is also the version's status, exactly
	// as for an app without variants; another variant's never is, so a
	// failed GPU build leaves the version deployed.
	h.logger = h.logger.With(slog.String("variant", ids.Variant), slog.Bool("defaultVariant", ids.VariantDefault))
	h.SendVariantStatusEvent(ctx, applicationId, deploymentId, ids.Variant, ids.VariantDefault, final, event.Detail.UpdatedAt)
	if ids.VariantDefault {
		if err := h.UpdateApplicationsTable(ctx, applicationId, final, applicationsTable); err != nil {
			return err
		}
	}
	return h.UpdateVariantStatus(ctx, applicationId, ids.Variant, final, VariantFailureReason(event), applicationsTable)
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

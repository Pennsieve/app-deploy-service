package handler

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	"github.com/aws/aws-sdk-go-v2/service/codebuild"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/pennsieve/app-deploy-service/status/models"
)

// Build environment variables the provisioner sets on each image build
// (fargate/app-provisioner/provisioner/imagebuild).
const (
	BuildDeploymentIdVar      = "DEPLOYMENT_ID"
	BuildApplicationIdVar     = "APPLICATION_ID"
	BuildApplicationsTableVar = "APPLICATIONS_TABLE"
	BuildGitTokenVar          = "GIT_TOKEN"
)

const (
	// Deployment versions for build events: a build reports IN_PROGRESS
	// once and then one final state, so the final state always wins.
	buildInProgressVersion = 1
	buildFinalVersion      = 2

	// failure reasons keep the last lines of the build's own output
	buildLogTailLines    = 25
	buildLogFetchLimit   = 200
	maxFailureReasonSize = 3500
)

// buildFinalStates maps CodeBuild's final states to "did the build fail".
var buildFinalStates = map[string]bool{
	models.BuildSucceeded: false,
	models.BuildFailed:    true,
	models.BuildFault:     true,
	models.BuildStopped:   true,
	models.BuildTimedOut:  true,
}

// HandleBuild records a CodeBuild image build's state change the way an ECS
// task state change is recorded for the kaniko task: the deployment gets
// RUNNING, then STOPPED with errored set, and the version becomes deployed or
// error. A failed build's reason (its failed phase, the tail of its log and a
// link to the log) goes in the deployment's stoppedReason.
func (h *DeployTaskStateChangeHandler) HandleBuild(ctx context.Context, event models.BuildStateChangeEvent) error {
	logger := h.logger.With(slog.String("buildId", event.Detail.BuildId), slog.String("buildStatus", event.Detail.BuildStatus))
	logger.Info("handling build event", slog.String("project", event.Detail.ProjectName), slog.String("phase", event.Detail.CurrentPhase))

	failed, final := buildFinalStates[event.Detail.BuildStatus]
	if !final && event.Detail.BuildStatus != models.BuildInProgress {
		logger.Warn("ignoring build event with unknown status")
		return nil
	}

	env, err := h.buildEnvironment(ctx, event)
	if err != nil {
		return err
	}
	ids := idsFromBuildVariables(env)
	if err := ids.CheckIds(); err != nil {
		return fmt.Errorf("build %s missing id variables: %w", event.Detail.BuildId, err)
	}

	reason := ""
	if final && failed {
		reason = h.buildFailureReason(ctx, event)
	}
	recordErr := h.recordStateChange(ctx, ids, BuildToTaskEvent(event, reason))
	if final {
		h.deleteGitToken(ctx, logger, env)
	}
	return recordErr
}

// BuildToTaskEvent expresses a build state change as the ECS task state
// change the deployments table and the rest of the handler already know.
func BuildToTaskEvent(event models.BuildStateChangeEvent, failureReason string) models.TaskStateChangeEvent {
	detail := event.Detail
	eventTime := event.Time
	started := detail.Additional.StartedAt()
	task := models.TaskStateChangeEvent{
		Id:         event.Id,
		Time:       event.Time,
		DetailType: event.DetailType,
		Region:     event.Region,
		Resources:  event.Resources,
		Source:     event.Source,
		Detail: models.Detail{
			TaskArn:   detail.BuildId,
			CreatedAt: started,
			StartedAt: started,
			UpdatedAt: &eventTime,
		},
	}
	failed, final := buildFinalStates[detail.BuildStatus]
	if !final {
		task.Detail.LastStatus = "RUNNING"
		task.Detail.DesiredStatus = "RUNNING"
		task.Detail.Version = buildInProgressVersion
		return task
	}
	task.Detail.LastStatus = models.StateStopped
	task.Detail.DesiredStatus = models.StateStopped
	task.Detail.Version = buildFinalVersion
	task.Detail.StoppedAt = &eventTime
	task.Detail.StopCode = detail.BuildStatus
	task.Detail.BuildFailed = failed
	task.Detail.StoppedReason = failureReason
	return task
}

// buildEnvironment returns the build's environment variables (deployment and
// application ids, token parameter): from the event, or from the build itself
// when the event doesn't carry the ids.
func (h *DeployTaskStateChangeHandler) buildEnvironment(ctx context.Context, event models.BuildStateChangeEvent) (models.BuildEnvironment, error) {
	env := event.Detail.Additional.Environment
	if idsFromBuildVariables(env).CheckIds() == nil {
		return env, nil
	}
	if h.CodeBuildApi == nil {
		return env, fmt.Errorf("build %s has no deployment ids in its event", event.Detail.BuildId)
	}
	out, err := h.CodeBuildApi.BatchGetBuilds(ctx, &codebuild.BatchGetBuildsInput{Ids: []string{event.Detail.BuildId}})
	if err != nil {
		return env, fmt.Errorf("error getting build %s: %w", event.Detail.BuildId, err)
	}
	if len(out.Builds) == 0 || out.Builds[0].Environment == nil {
		return env, fmt.Errorf("build %s not found", event.Detail.BuildId)
	}
	env = models.BuildEnvironment{}
	for _, v := range out.Builds[0].Environment.EnvironmentVariables {
		env.EnvironmentVariables = append(env.EnvironmentVariables, models.BuildEnvironmentVariable{
			Name: aws.ToString(v.Name), Type: string(v.Type), Value: aws.ToString(v.Value),
		})
	}
	return env, nil
}

func idsFromBuildVariables(env models.BuildEnvironment) DeploymentApplicationIds {
	value := func(name string) string {
		v, _ := env.Variable(name)
		return v.Value
	}
	return DeploymentApplicationIds{
		DeploymentId:      value(BuildDeploymentIdVar),
		ApplicationId:     value(BuildApplicationIdVar),
		ApplicationsTable: value(BuildApplicationsTableVar),
	}
}

// buildFailureReason says why a build failed: its status and failed phase,
// that phase's context, the last lines of the build's own output and a link
// to the whole log.
func (h *DeployTaskStateChangeHandler) buildFailureReason(ctx context.Context, event models.BuildStateChangeEvent) string {
	detail := event.Detail
	var b strings.Builder
	fmt.Fprintf(&b, "image build %s", detail.BuildStatus)
	for _, phase := range detail.Additional.Phases {
		if phase.PhaseStatus == "" || phase.PhaseStatus == models.BuildSucceeded {
			continue
		}
		fmt.Fprintf(&b, " in phase %s", phase.PhaseType)
		for _, c := range phase.PhaseContext {
			if c = strings.TrimSpace(c); c != "" && c != ":" {
				fmt.Fprintf(&b, ": %s", c)
			}
		}
		break
	}
	if tail := h.buildLogTail(ctx, detail.Additional.Logs); tail != "" {
		b.WriteString("\n")
		b.WriteString(tail)
	}
	reason := b.String()
	link := ""
	if detail.Additional.Logs.DeepLink != "" {
		link = "\nlog: " + detail.Additional.Logs.DeepLink
	}
	if room := maxFailureReasonSize - len(link); len(reason) > room {
		// keep the head (status, phase) and the end of the log tail
		head := strings.SplitN(reason, "\n", 2)[0]
		keep := room - len(head) - len("\n…")
		if keep < 0 {
			keep = 0
		}
		reason = head + "\n…" + reason[len(reason)-keep:]
	}
	return reason + link
}

// buildLogTail returns the last lines the build itself printed, without
// CodeBuild's own "[Container]" lines. "" when the log can't be read.
func (h *DeployTaskStateChangeHandler) buildLogTail(ctx context.Context, logs models.BuildLogs) string {
	if h.LogsApi == nil || logs.GroupName == "" || logs.StreamName == "" {
		return ""
	}
	out, err := h.LogsApi.GetLogEvents(ctx, &cloudwatchlogs.GetLogEventsInput{
		LogGroupName:  aws.String(logs.GroupName),
		LogStreamName: aws.String(logs.StreamName),
		StartFromHead: aws.Bool(false),
		Limit:         aws.Int32(buildLogFetchLimit),
	})
	if err != nil {
		h.logger.Warn("unable to read build log", slog.String("group", logs.GroupName), slog.String("stream", logs.StreamName), slog.Any("error", err))
		return ""
	}
	var lines []string
	for _, e := range out.Events {
		for _, line := range strings.Split(strings.TrimRight(aws.ToString(e.Message), "\n"), "\n") {
			if strings.HasPrefix(line, "[Container]") || strings.TrimSpace(line) == "" {
				continue
			}
			lines = append(lines, line)
		}
	}
	if len(lines) > buildLogTailLines {
		lines = lines[len(lines)-buildLogTailLines:]
	}
	return strings.Join(lines, "\n")
}

// deleteGitToken deletes the SSM parameter that held a private repo's token
// for the build. Best effort: a failure is logged, never retried.
func (h *DeployTaskStateChangeHandler) deleteGitToken(ctx context.Context, logger *slog.Logger, env models.BuildEnvironment) {
	v, ok := env.Variable(BuildGitTokenVar)
	if !ok || v.Type != "PARAMETER_STORE" || v.Value == "" || h.SSMApi == nil {
		return
	}
	if _, err := h.SSMApi.DeleteParameter(ctx, &ssm.DeleteParameterInput{Name: aws.String(v.Value)}); err != nil {
		logger.Warn("unable to delete the build's GitHub token parameter", slog.String("parameter", v.Value), slog.Any("error", err))
	}
}

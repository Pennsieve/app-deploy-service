package models

import "time"

// CodeBuild build states, as "build-status" in a Build State Change event.
const (
	BuildInProgress = "IN_PROGRESS"
	BuildSucceeded  = "SUCCEEDED"
	BuildFailed     = "FAILED"
	BuildFault      = "FAULT"
	BuildStopped    = "STOPPED"
	BuildTimedOut   = "TIMED_OUT"
)

// CodeBuildSource and BuildStateChangeDetailType identify a CodeBuild
// "Build State Change" event from the App Store image-builder project.
const (
	CodeBuildSource            = "aws.codebuild"
	BuildStateChangeDetailType = "CodeBuild Build State Change"
)

// BuildStateChangeEvent is the EventBridge event CodeBuild sends when a build
// changes state. Like TaskStateChangeEvent, it only has the fields we use.
// https://docs.aws.amazon.com/codebuild/latest/userguide/sample-build-notifications.html
type BuildStateChangeEvent struct {
	Id         string      `json:"id"`
	DetailType string      `json:"detail-type"`
	Source     string      `json:"source"`
	Time       time.Time   `json:"time"`
	Region     string      `json:"region"`
	Resources  []string    `json:"resources"`
	Detail     BuildDetail `json:"detail"`
}

type BuildDetail struct {
	BuildStatus  string                `json:"build-status"`
	ProjectName  string                `json:"project-name"`
	BuildId      string                `json:"build-id"`
	CurrentPhase string                `json:"current-phase"`
	Additional   BuildAdditionalDetail `json:"additional-information"`
}

type BuildAdditionalDetail struct {
	BuildComplete bool             `json:"build-complete"`
	StartTime     string           `json:"build-start-time"`
	Logs          BuildLogs        `json:"logs"`
	Phases        []BuildPhase     `json:"phases"`
	Environment   BuildEnvironment `json:"environment"`
}

type BuildLogs struct {
	GroupName  string `json:"group-name"`
	StreamName string `json:"stream-name"`
	DeepLink   string `json:"deep-link"`
}

type BuildPhase struct {
	PhaseType    string   `json:"phase-type"`
	PhaseStatus  string   `json:"phase-status"`
	PhaseContext []string `json:"phase-context"`
}

type BuildEnvironment struct {
	ComputeType          string                     `json:"compute-type"`
	EnvironmentVariables []BuildEnvironmentVariable `json:"environment-variables"`
}

type BuildEnvironmentVariable struct {
	Name  string `json:"name"`
	Type  string `json:"type"`
	Value string `json:"value"`
}

// buildStartTimeLayout is how CodeBuild writes build-start-time in events.
const buildStartTimeLayout = "Jan 2, 2006 3:04:05 PM"

// StartedAt parses build-start-time (UTC); nil when absent or unparseable.
func (d BuildAdditionalDetail) StartedAt() *time.Time {
	t, err := time.Parse(buildStartTimeLayout, d.StartTime)
	if err != nil {
		return nil
	}
	return &t
}

// Variable returns a build environment variable, and whether the build has it.
func (e BuildEnvironment) Variable(name string) (BuildEnvironmentVariable, bool) {
	for _, v := range e.EnvironmentVariables {
		if v.Name == name {
			return v, true
		}
	}
	return BuildEnvironmentVariable{}, false
}

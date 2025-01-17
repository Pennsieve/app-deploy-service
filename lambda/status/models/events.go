package models

import "time"

// TaskStateChangeEvent is the ECS task state change event this Lambda handles.
// There does not seem to be a struct predefined for this in the Go AWS SDK.
// This struct mostly only contains the fields we are interested in. The actual event
// contains more information.
type TaskStateChangeEvent struct {
	Id         string    `json:"id"`
	Version    string    `json:"version"`
	Time       time.Time `json:"time"`
	DetailType string    `json:"detail-type"`
	Region     string    `json:"region"`
	Resources  []string  `json:"resources"`
	Source     string    `json:"source"`
	Account    string    `json:"account"`

	Detail Detail `json:"detail"`
}

// Detail contains most of the info we are really interested in.
// Docs for time fields came from https://docs.aws.amazon.com/AmazonECS/latest/APIReference/API_Task.html
// which is not for the actual event object, but the closest thing to a reference for this that I could find.
// Times are pointers because they are all not required. And zero times are not considered empty by
// the omitempty specifier on the json tag.
type Detail struct {
	LastStatus    string `json:"lastStatus"`
	DesiredStatus string `json:"desiredStatus"`

	TaskArn           string `json:"taskArn"`
	TaskDefinitionArn string `json:"taskDefinitionArn"`
	Version           int    `json:"version"`

	Containers []Container `json:"containers"`
	// CreatedAt The timestamp for the time when the task was created.
	// More specifically, it's for the time when the task entered the PENDING state.
	CreatedAt *time.Time `json:"createdAt,omitempty"`

	// StartedAt The timestamp for the time when the task started.
	// More specifically, it's for the time when the task transitioned from the PENDING state to the RUNNING state.
	StartedAt *time.Time `json:"startedAt,omitempty"`

	// UpdatedAt is not in the reference. Assume it is the time this state change happened.
	UpdatedAt *time.Time `json:"updatedAt,omitempty"`

	// StoppingAt The timestamp for the time when the task stops.
	// More specifically, it's for the time when the task transitions from the RUNNING state to STOPPING.
	StoppingAt *time.Time `json:"stoppingAt,omitempty"`
	// StoppedAt The timestamp for the time when the task was stopped.
	// More specifically, it's for the time when the task transitioned from the RUNNING state to the STOPPED state.
	StoppedAt *time.Time `json:"stoppedAt,omitempty"`

	// ExecutionStoppedAt The timestamp for the time when the task execution stopped.
	ExecutionStoppedAt *time.Time `json:"executionStoppedAt,omitempty"`

	StopCode      string `json:"stopCode"`
	StoppedReason string `json:"stoppedReason"`
}

// Container may be the only way we can distinguish an error during kaniko's run.
// The StopCode in Detail seems to be "EssentialContainerExited" both
// when the deployment task succeeded and when it failed for something like a
// compilation error in the user's application. But the ExitCode will be
// 1 in the later case, and 0 if kaniko succeeded.
type Container struct {
	ExitCode   int    `json:"exitCode"`
	Image      string `json:"image"`
	LastStatus string `json:"lastStatus"`
	TaskArn    string `json:"taskArn"`
}

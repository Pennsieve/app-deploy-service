package store_dynamodb

import "time"

// *Field consts should match the dynamodbav struct tag for the field

const DeploymentKeyField = "id"

type DeploymentKey struct {
	Id string `dynamodbav:"id"`
}

type Deployment struct {
	DeploymentKey
	ApplicationId   string    `dynamodbav:"applicationId"`
	InitiatedAt     time.Time `dynamodbav:"initiatedAt"`
	WorkspaceNodeId string    `dynamodbav:"workspaceNodeId"`
	UserNodeId      string    `dynamodbav:"userNodeId"`
	Action          string    `dynamodbav:"action"`
	Version         int       `dynamodbav:"version"`
	LastStatus      string    `dynamodbav:"lastStatus"`
	DesiredStatus   string    `dynamodbav:"desiredStatus"`
	TaskArn         string    `dynamodbav:"taskArn"`

	// UpdatedAt is not in the reference. Assume it is the time this state change happened.
	UpdatedAt *time.Time `dynamodbav:"updatedAt,omitempty"`

	// CreatedAt The timestamp for the time when the task was created.
	// More specifically, it's for the time when the task entered the PENDING state.
	CreatedAt *time.Time `dynamodbav:"createdAt,omitempty"`

	// StartedAt The timestamp for the time when the task started.
	// More specifically, it's for the time when the task transitioned from the PENDING state to the RUNNING state.
	StartedAt *time.Time `dynamodbav:"startedAt,omitempty"`

	// StoppedAt The timestamp for the time when the task was stopped.
	// More specifically, it's for the time when the task transitioned from the RUNNING state to the STOPPED state.
	StoppedAt *time.Time `dynamodbav:"stoppedAt,omitempty"`

	StopCode      string `dynamodbav:"stopCode,omitempty"`
	StoppedReason string `dynamodbav:"stoppedReason,omitempty"`
	Errored       bool   `dynamodbav:"errored,omitempty"`
}

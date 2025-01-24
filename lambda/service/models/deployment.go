package models

import "time"

type Deployment struct {
	DeploymentId  string    `json:"deploymentId"`
	ApplicationId string    `json:"applicationId"`
	InitiatedAt   time.Time `json:"initiatedAt"`
	Version       int       `json:"version"`
	LastStatus    string    `json:"lastStatus"`
	DesiredStatus string    `json:"desiredStatus"`
	TaskArn       string    `json:"taskArn"`

	// UpdatedAt is not in the reference. Assume it is the time this state change happened.
	UpdatedAt *time.Time `json:"updatedAt,omitempty"`

	// CreatedAt The timestamp for the time when the task was created.
	// More specifically, it's for the time when the task entered the PENDING state.
	CreatedAt *time.Time `json:"createdAt,omitempty"`

	// StartedAt The timestamp for the time when the task started.
	// More specifically, it's for the time when the task transitioned from the PENDING state to the RUNNING state.
	StartedAt *time.Time `json:"startedAt,omitempty"`

	// StoppedAt The timestamp for the time when the task was stopped.
	// More specifically, it's for the time when the task transitioned from the RUNNING state to the STOPPED state.
	StoppedAt *time.Time `json:"stoppedAt,omitempty"`

	StopCode      string `json:"stopCode,omitempty"`
	StoppedReason string `json:"stoppedReason,omitempty"`
	Errored       bool   `json:"errored,omitempty"`
}

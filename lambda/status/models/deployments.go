package models

import (
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/pennsieve/app-deploy-service/status/dydbutils"
	"time"
)

// These *Field const must match the values in the dynamodbav struct tags in Deployment

const DeploymentApplicationIdField = "applicationId"
const DeploymentIdField = "deploymentId"
const DeploymentVersionField = "version"
const DeploymentTaskArnField = "taskArn"
const DeploymentLastStatusField = "lastStatus"
const DeploymentDesiredStatusField = "desiredStatus"
const DeploymentCreatedAtField = "createdAt"
const DeploymentStartedAtField = "startedAt"
const DeploymentUpdatedAtField = "updatedAt"
const DeploymentStoppedAtField = "stoppedAt"
const DeploymentStopCodeField = "stopCode"
const DeploymentStoppedReasonField = "stoppedReason"
const DeploymentErroredField = "errored"

type DeploymentKey struct {
	ApplicationId string `dynamodbav:"applicationId"`
	DeploymentId  string `dynamodbav:"deploymentId"`
}

type Deployment struct {
	DeploymentKey
	Version       int    `dynamodbav:"version"`
	LastStatus    string `dynamodbav:"lastStatus"`
	DesiredStatus string `dynamodbav:"desiredStatus"`
	TaskArn       string `dynamodbav:"taskArn"`

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

func DeploymentKeyItem(applicationId, deploymentId string) map[string]types.AttributeValue {
	return map[string]types.AttributeValue{
		DeploymentApplicationIdField: dydbutils.StringAttributeValue(applicationId),
		DeploymentIdField:            dydbutils.StringAttributeValue(deploymentId),
	}
}

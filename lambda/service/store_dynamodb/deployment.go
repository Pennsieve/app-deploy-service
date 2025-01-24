package store_dynamodb

import "time"

type DeploymentKey struct {
	ApplicationId string `dynamodbav:"applicationId"`
	DeploymentId  string `dynamodbav:"deploymentId"`
}

type Deployment struct {
	DeploymentKey
	InitiatedAt     time.Time `dynamodbav:"initiatedAt"`
	WorkspaceNodeId string    `dynamodbav:"workspaceNodeId"`
	UserNodeId      string    `dynamodbav:"userNodeId"`
	Action          string    `dynamodbav:"action"`
	LastStatus      string    `dynamodbav:"lastStatus"`
	Errored         bool      `dynamodbav:"errored,omitempty"`
}

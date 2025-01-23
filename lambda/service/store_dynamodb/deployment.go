package store_dynamodb

import "time"

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
	LastStatus      string    `dynamodbav:"lastStatus"`
	Errored         bool      `dynamodbav:"errored,omitempty"`
}

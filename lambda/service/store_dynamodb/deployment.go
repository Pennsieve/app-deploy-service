package store_dynamodb

import "time"

type Deployment struct {
	Id              string    `dynamodbav:"id"`
	ApplicationId   string    `dynamodbav:"applicationId"`
	CreatedAt       time.Time `dynamodbav:"createdAt"`
	WorkspaceNodeId string    `dynamodbav:"workspaceNodeId"`
	UserNodeId      string    `dynamodbav:"userNodeId"`
}

package store_dynamodb

const DeploymentIdField = "deploymentId"
const DeploymentApplicationIdField = "applicationId"

type DeploymentKey struct {
	ApplicationId string `dynamodbav:"applicationId"`
	DeploymentId  string `dynamodbav:"deploymentId"`
}

type Deployment struct {
	DeploymentKey
	Errored bool `dynamodbav:"errored,omitempty"`
}

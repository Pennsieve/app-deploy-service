package store_dynamodb

type DeploymentKey struct {
	ApplicationId string `dynamodbav:"applicationId"`
	DeploymentId  string `dynamodbav:"deploymentId"`
}

type Deployment struct {
	DeploymentKey
	Errored bool `dynamodbav:"errored,omitempty"`
}

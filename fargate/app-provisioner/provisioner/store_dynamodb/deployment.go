package store_dynamodb

type DeploymentKey struct {
	Id string `dynamodbav:"id"`
}

type Deployment struct {
	DeploymentKey
	Errored bool `dynamodbav:"errored,omitempty"`
}

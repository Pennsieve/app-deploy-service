package models

type Deployment struct {
	Id            string `dynamodbav:"id"`
	ApplicationId string `dynamodbav:"applicationId"`
}

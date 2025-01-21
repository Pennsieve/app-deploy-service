package models

import (
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/pennsieve/app-deploy-service/status/dydbutils"
)

type Deployment struct {
	Id            string `dynamodbav:"id"`
	ApplicationId string `dynamodbav:"applicationId"`
}

func DeploymentKey(deploymentId string) map[string]types.AttributeValue {
	return map[string]types.AttributeValue{"id": dydbutils.StringAttributeValue(deploymentId)}
}

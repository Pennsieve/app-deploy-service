package store_dynamodb

import (
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

type App struct {
	Uuid            string `dynamodbav:"uuid"`
	Name            string `dynamodbav:"name"`
	Description     string `dynamodbav:"description"`
	AppEcrUrl       string `dynamodbav:"appUrl"`
	ComputeNodeUuid string `dynamodbav:"computeNodeUuid"`
	CreatedAt       string `dynamodbav:"createdAt"`
	OrganizationId  string `dynamodbav:"organizationId"`
	UserId          string `dynamodbav:"userId"`
}

func (i App) GetKey() map[string]types.AttributeValue {
	uuid, err := attributevalue.Marshal(i.Uuid)
	if err != nil {
		panic(err)
	}

	return map[string]types.AttributeValue{"uuid": uuid}
}

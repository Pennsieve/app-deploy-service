package store_dynamodb

import (
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

type Application struct {
	Uuid           string `dynamodbav:"uuid"`
	Name           string `dynamodbav:"name"`
	Description    string `dynamodbav:"description"`
	AppEcrUrl      string `dynamodbav:"appUrl"`
	Env            string `dynamodbav:"environment"`
	OrganizationId string `dynamodbav:"organizationId"`
	UserId         string `dynamodbav:"userId"`
	CreatedAt      string `dynamodbav:"createdAt"`
}

type DeleteNode struct {
	Uuid string `dynamodbav:"uuid"`
}

func (i Application) GetKey() map[string]types.AttributeValue {
	uuid, err := attributevalue.Marshal(i.Uuid)
	if err != nil {
		panic(err)
	}

	return map[string]types.AttributeValue{"uuid": uuid}
}

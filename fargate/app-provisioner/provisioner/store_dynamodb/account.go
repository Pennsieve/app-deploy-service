package store_dynamodb

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

type Account struct {
	Uuid     string `dynamodbav:"uuid"`
	RoleName string `dynamodbav:"roleName"`
}

type AccountStore struct {
	DB        *dynamodb.Client
	TableName string
}

func NewAccountStore(db *dynamodb.Client, tableName string) *AccountStore {
	return &AccountStore{db, tableName}
}

func (s *AccountStore) GetById(ctx context.Context, uuid string) (Account, error) {
	key, err := attributevalue.Marshal(uuid)
	if err != nil {
		return Account{}, fmt.Errorf("error marshaling account key: %w", err)
	}

	response, err := s.DB.GetItem(ctx, &dynamodb.GetItemInput{
		Key:       map[string]types.AttributeValue{"uuid": key},
		TableName: aws.String(s.TableName),
	})
	if err != nil {
		return Account{}, fmt.Errorf("error getting account: %w", err)
	}
	if response.Item == nil {
		return Account{}, fmt.Errorf("account not found: %s", uuid)
	}

	var account Account
	err = attributevalue.UnmarshalMap(response.Item, &account)
	if err != nil {
		return Account{}, fmt.Errorf("error unmarshaling account: %w", err)
	}

	return account, nil
}

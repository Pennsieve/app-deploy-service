package store_dynamodb

import (
	"context"
	"fmt"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// DeploymentsTableAPI is an interface only containing the
// DynamoDB client methods used by DeploymentsStore
type DeploymentsTableAPI interface {
	PutItem(ctx context.Context, params *dynamodb.PutItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error)
	UpdateItem(ctx context.Context, params *dynamodb.UpdateItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.UpdateItemOutput, error)
}

type DeploymentsStore struct {
	api       DeploymentsTableAPI
	tableName string
}

func NewDeploymentsStore(api DeploymentsTableAPI, tableName string) *DeploymentsStore {
	return &DeploymentsStore{
		api:       api,
		tableName: tableName,
	}
}

func (s *DeploymentsStore) Insert(ctx context.Context, newDeployment Deployment) error {
	item, err := attributevalue.MarshalMap(newDeployment)
	if err != nil {
		return fmt.Errorf("error marshaling deployment: %w", err)
	}
	_, err = s.api.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(s.tableName), Item: item,
	})
	if err != nil {
		return fmt.Errorf("error inserting deployment: %w", err)
	}

	return nil
}

func (s *DeploymentsStore) SetErrored(ctx context.Context, applicationId string, deploymentId string) error {
	key, err := attributevalue.MarshalMap(DeploymentKey{
		ApplicationId: applicationId,
		DeploymentId:  deploymentId,
	})
	if err != nil {
		return fmt.Errorf("error marshaling key for deployment errored update: %w", err)
	}

	_, err = s.api.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName: aws.String(s.tableName),
		Key:       key,
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":e": &types.AttributeValueMemberBOOL{Value: true},
		},
		UpdateExpression: aws.String("set errored = :e"),
	})
	if err != nil {
		return fmt.Errorf("error updating deployment errored: %w", err)
	}

	return nil
}

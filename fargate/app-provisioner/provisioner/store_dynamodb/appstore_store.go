package store_dynamodb

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// AppStoreVersionDBStore operates on the appstore versions table.
type AppStoreVersionDBStore interface {
	UpdateStatus(ctx context.Context, newStatus string, uuid string) error
	UpdateDestinationUrl(ctx context.Context, uuid string, destinationUrl string, status string) error
}

type AppStoreVersionDatabaseStore struct {
	DB        *dynamodb.Client
	TableName string
}

func NewAppStoreVersionDatabaseStore(db *dynamodb.Client, tableName string) *AppStoreVersionDatabaseStore {
	return &AppStoreVersionDatabaseStore{db, tableName}
}

func (r *AppStoreVersionDatabaseStore) UpdateStatus(ctx context.Context, newStatus string, uuid string) error {
	key, err := attributevalue.MarshalMap(ApplicationKey{Uuid: uuid})
	if err != nil {
		return fmt.Errorf("error marshaling key for version status update: %w", err)
	}

	_, err = r.DB.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName: aws.String(r.TableName),
		Key:       key,
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":s": &types.AttributeValueMemberS{Value: newStatus},
		},
		UpdateExpression: aws.String("set registrationStatus = :s"),
	})
	if err != nil {
		return fmt.Errorf("error updating appstore version status: %w", err)
	}

	return nil
}

func (r *AppStoreVersionDatabaseStore) UpdateDestinationUrl(ctx context.Context, uuid string, destinationUrl string, status string) error {
	key, err := attributevalue.MarshalMap(ApplicationKey{Uuid: uuid})
	if err != nil {
		return fmt.Errorf("error marshaling key for version destination update: %w", err)
	}

	_, err = r.DB.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName: aws.String(r.TableName),
		Key:       key,
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":d": &types.AttributeValueMemberS{Value: destinationUrl},
			":s": &types.AttributeValueMemberS{Value: status},
		},
		UpdateExpression: aws.String("set destinationUrl = :d, registrationStatus = :s"),
	})
	if err != nil {
		return fmt.Errorf("error updating appstore version destination URL: %w", err)
	}

	return nil
}

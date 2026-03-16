package store_dynamodb

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/expression"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

type AppAccessTableAPI interface {
	PutItem(ctx context.Context, params *dynamodb.PutItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error)
	Query(ctx context.Context, params *dynamodb.QueryInput, optFns ...func(*dynamodb.Options)) (*dynamodb.QueryOutput, error)
	DeleteItem(ctx context.Context, params *dynamodb.DeleteItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.DeleteItemOutput, error)
	BatchWriteItem(ctx context.Context, params *dynamodb.BatchWriteItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.BatchWriteItemOutput, error)
}

type AppAccessDBStore interface {
	GetByApp(context.Context, string) ([]AppAccess, error)
	GetByEntity(context.Context, string) ([]AppAccess, error)
	GetAccess(context.Context, string, string) (*AppAccess, error)
	Insert(context.Context, AppAccess) error
	Delete(context.Context, string, string) error
	ReplaceByApp(context.Context, string, []AppAccess) error
}

type AppAccessDatabaseStore struct {
	api       AppAccessTableAPI
	TableName string
}

func NewAppAccessDatabaseStore(api AppAccessTableAPI, tableName string) *AppAccessDatabaseStore {
	return &AppAccessDatabaseStore{api, tableName}
}

func (r *AppAccessDatabaseStore) GetByApp(ctx context.Context, appUuid string) ([]AppAccess, error) {
	appId := fmt.Sprintf("app#%s", appUuid)
	keyCondition := expression.Key("appId").Equal(expression.Value(appId))
	expr, err := expression.NewBuilder().WithKeyCondition(keyCondition).Build()
	if err != nil {
		return nil, fmt.Errorf("error building expression: %w", err)
	}

	response, err := r.api.Query(ctx, &dynamodb.QueryInput{
		TableName:                 aws.String(r.TableName),
		IndexName:                 aws.String("appId-entityId-index"),
		ExpressionAttributeNames:  expr.Names(),
		ExpressionAttributeValues: expr.Values(),
		KeyConditionExpression:    expr.KeyCondition(),
	})
	if err != nil {
		return nil, fmt.Errorf("error querying app access by appId: %w", err)
	}

	var items []AppAccess
	err = attributevalue.UnmarshalListOfMaps(response.Items, &items)
	if err != nil {
		return nil, fmt.Errorf("error unmarshaling app access: %w", err)
	}
	return items, nil
}

func (r *AppAccessDatabaseStore) GetByEntity(ctx context.Context, entityId string) ([]AppAccess, error) {
	keyCondition := expression.Key("entityId").Equal(expression.Value(entityId))
	expr, err := expression.NewBuilder().WithKeyCondition(keyCondition).Build()
	if err != nil {
		return nil, fmt.Errorf("error building expression: %w", err)
	}

	response, err := r.api.Query(ctx, &dynamodb.QueryInput{
		TableName:                 aws.String(r.TableName),
		ExpressionAttributeNames:  expr.Names(),
		ExpressionAttributeValues: expr.Values(),
		KeyConditionExpression:    expr.KeyCondition(),
	})
	if err != nil {
		return nil, fmt.Errorf("error querying app access by entityId: %w", err)
	}

	var items []AppAccess
	err = attributevalue.UnmarshalListOfMaps(response.Items, &items)
	if err != nil {
		return nil, fmt.Errorf("error unmarshaling app access: %w", err)
	}
	return items, nil
}

func (r *AppAccessDatabaseStore) GetAccess(ctx context.Context, entityId string, appId string) (*AppAccess, error) {
	keyCondition := expression.Key("entityId").Equal(expression.Value(entityId)).
		And(expression.Key("appId").Equal(expression.Value(appId)))
	expr, err := expression.NewBuilder().WithKeyCondition(keyCondition).Build()
	if err != nil {
		return nil, fmt.Errorf("error building expression: %w", err)
	}

	response, err := r.api.Query(ctx, &dynamodb.QueryInput{
		TableName:                 aws.String(r.TableName),
		ExpressionAttributeNames:  expr.Names(),
		ExpressionAttributeValues: expr.Values(),
		KeyConditionExpression:    expr.KeyCondition(),
	})
	if err != nil {
		return nil, fmt.Errorf("error querying app access: %w", err)
	}

	if len(response.Items) == 0 {
		return nil, nil
	}

	var item AppAccess
	err = attributevalue.UnmarshalMap(response.Items[0], &item)
	if err != nil {
		return nil, fmt.Errorf("error unmarshaling app access: %w", err)
	}
	return &item, nil
}

func (r *AppAccessDatabaseStore) Insert(ctx context.Context, access AppAccess) error {
	item, err := attributevalue.MarshalMap(access)
	if err != nil {
		return fmt.Errorf("error marshaling app access: %w", err)
	}
	_, err = r.api.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(r.TableName), Item: item,
	})
	if err != nil {
		return fmt.Errorf("error inserting app access: %w", err)
	}
	return nil
}

func (r *AppAccessDatabaseStore) ReplaceByApp(ctx context.Context, appUuid string, newEntries []AppAccess) error {
	existing, err := r.GetByApp(ctx, appUuid)
	if err != nil {
		return fmt.Errorf("error fetching existing access entries: %w", err)
	}

	var writeRequests []types.WriteRequest

	for _, entry := range existing {
		entityIdAv, err := attributevalue.Marshal(entry.EntityId)
		if err != nil {
			return fmt.Errorf("error marshaling entityId for delete: %w", err)
		}
		appIdAv, err := attributevalue.Marshal(entry.AppId)
		if err != nil {
			return fmt.Errorf("error marshaling appId for delete: %w", err)
		}
		writeRequests = append(writeRequests, types.WriteRequest{
			DeleteRequest: &types.DeleteRequest{
				Key: map[string]types.AttributeValue{
					"entityId": entityIdAv,
					"appId":    appIdAv,
				},
			},
		})
	}

	for _, entry := range newEntries {
		item, err := attributevalue.MarshalMap(entry)
		if err != nil {
			return fmt.Errorf("error marshaling access entry: %w", err)
		}
		writeRequests = append(writeRequests, types.WriteRequest{
			PutRequest: &types.PutRequest{Item: item},
		})
	}

	if len(writeRequests) == 0 {
		return nil
	}

	const maxBatchSize = 25
	for i := 0; i < len(writeRequests); i += maxBatchSize {
		end := i + maxBatchSize
		if end > len(writeRequests) {
			end = len(writeRequests)
		}
		batch := writeRequests[i:end]
		_, err := r.api.BatchWriteItem(ctx, &dynamodb.BatchWriteItemInput{
			RequestItems: map[string][]types.WriteRequest{
				r.TableName: batch,
			},
		})
		if err != nil {
			return fmt.Errorf("error batch writing app access: %w", err)
		}
	}

	return nil
}

func (r *AppAccessDatabaseStore) Delete(ctx context.Context, entityId string, appId string) error {
	entityIdAv, err := attributevalue.Marshal(entityId)
	if err != nil {
		return fmt.Errorf("error marshaling entityId: %w", err)
	}
	appIdAv, err := attributevalue.Marshal(appId)
	if err != nil {
		return fmt.Errorf("error marshaling appId: %w", err)
	}

	_, err = r.api.DeleteItem(ctx, &dynamodb.DeleteItemInput{
		TableName: aws.String(r.TableName),
		Key: map[string]types.AttributeValue{
			"entityId": entityIdAv,
			"appId":    appIdAv,
		},
	})
	if err != nil {
		return fmt.Errorf("error deleting app access: %w", err)
	}
	return nil
}

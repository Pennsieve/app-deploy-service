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

// AppStoreVersionTableAPI is a narrow interface containing only the DynamoDB client methods used by AppStoreVersionDatabaseStore.
type AppStoreVersionTableAPI interface {
	PutItem(ctx context.Context, params *dynamodb.PutItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error)
	Query(ctx context.Context, params *dynamodb.QueryInput, optFns ...func(*dynamodb.Options)) (*dynamodb.QueryOutput, error)
	UpdateItem(ctx context.Context, params *dynamodb.UpdateItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.UpdateItemOutput, error)
}

// AppStoreVersionDBStore operates on the appstore versions table.
type AppStoreVersionDBStore interface {
	GetByApplicationId(context.Context, string) ([]AppStoreVersion, error)
	GetByApplicationIdAndVersion(ctx context.Context, applicationId string, version string) ([]AppStoreVersion, error)
	Insert(context.Context, AppStoreVersion) error
	UpdateStatus(ctx context.Context, newStatus string, uuid string) error
}

type AppStoreVersionDatabaseStore struct {
	api       AppStoreVersionTableAPI
	TableName string
}

func NewAppStoreVersionDatabaseStore(api AppStoreVersionTableAPI, tableName string) *AppStoreVersionDatabaseStore {
	return &AppStoreVersionDatabaseStore{api, tableName}
}

// GetByApplicationId returns all versions for a given application using the applicationId-version-index GSI.
func (r *AppStoreVersionDatabaseStore) GetByApplicationId(ctx context.Context, applicationId string) ([]AppStoreVersion, error) {
	versions := []AppStoreVersion{}

	keyCondition := expression.Key("applicationId").Equal(expression.Value(applicationId))
	expr, err := expression.NewBuilder().WithKeyCondition(keyCondition).Build()
	if err != nil {
		return versions, fmt.Errorf("error building expression: %w", err)
	}

	response, err := r.api.Query(ctx, &dynamodb.QueryInput{
		TableName:                 aws.String(r.TableName),
		IndexName:                 aws.String("applicationId-version-index"),
		ExpressionAttributeNames:  expr.Names(),
		ExpressionAttributeValues: expr.Values(),
		KeyConditionExpression:    expr.KeyCondition(),
	})
	if err != nil {
		return versions, fmt.Errorf("error querying appstore versions by applicationId: %w", err)
	}

	err = attributevalue.UnmarshalListOfMaps(response.Items, &versions)
	if err != nil {
		return versions, fmt.Errorf("error unmarshaling appstore versions: %w", err)
	}

	return versions, nil
}

// GetByApplicationIdAndVersion returns a specific version using the applicationId-version-index GSI.
func (r *AppStoreVersionDatabaseStore) GetByApplicationIdAndVersion(ctx context.Context, applicationId string, version string) ([]AppStoreVersion, error) {
	versions := []AppStoreVersion{}

	keyCondition := expression.Key("applicationId").Equal(expression.Value(applicationId)).
		And(expression.Key("version").Equal(expression.Value(version)))
	expr, err := expression.NewBuilder().WithKeyCondition(keyCondition).Build()
	if err != nil {
		return versions, fmt.Errorf("error building expression: %w", err)
	}

	response, err := r.api.Query(ctx, &dynamodb.QueryInput{
		TableName:                 aws.String(r.TableName),
		IndexName:                 aws.String("applicationId-version-index"),
		ExpressionAttributeNames:  expr.Names(),
		ExpressionAttributeValues: expr.Values(),
		KeyConditionExpression:    expr.KeyCondition(),
	})
	if err != nil {
		return versions, fmt.Errorf("error querying appstore version: %w", err)
	}

	err = attributevalue.UnmarshalListOfMaps(response.Items, &versions)
	if err != nil {
		return versions, fmt.Errorf("error unmarshaling appstore version: %w", err)
	}

	return versions, nil
}

func (r *AppStoreVersionDatabaseStore) Insert(ctx context.Context, version AppStoreVersion) error {
	item, err := attributevalue.MarshalMap(version)
	if err != nil {
		return fmt.Errorf("error marshaling appstore version: %w", err)
	}
	_, err = r.api.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(r.TableName), Item: item,
	})
	if err != nil {
		return fmt.Errorf("error inserting appstore version: %w", err)
	}

	return nil
}

func (r *AppStoreVersionDatabaseStore) UpdateStatus(ctx context.Context, newStatus string, uuid string) error {
	key, err := attributevalue.MarshalMap(ApplicationKey{Uuid: uuid})
	if err != nil {
		return fmt.Errorf("error marshaling key for appstore version status update: %w", err)
	}

	_, err = r.api.UpdateItem(ctx, &dynamodb.UpdateItemInput{
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

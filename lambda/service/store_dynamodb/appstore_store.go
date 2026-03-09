package store_dynamodb

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/expression"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
)

// AppStoreTableAPI is a narrow interface containing only the DynamoDB client methods used by AppStoreDatabaseStore.
type AppStoreTableAPI interface {
	PutItem(ctx context.Context, params *dynamodb.PutItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error)
	Query(ctx context.Context, params *dynamodb.QueryInput, optFns ...func(*dynamodb.Options)) (*dynamodb.QueryOutput, error)
	Scan(ctx context.Context, params *dynamodb.ScanInput, optFns ...func(*dynamodb.Options)) (*dynamodb.ScanOutput, error)
}

// AppStoreDBStore operates on the appstore applications table (one record per app/sourceUrl).
type AppStoreDBStore interface {
	GetBySourceUrl(context.Context, string) ([]AppStoreApplication, error)
	GetAll(context.Context) ([]AppStoreApplication, error)
	Insert(context.Context, AppStoreApplication) error
}

type AppStoreDatabaseStore struct {
	api       AppStoreTableAPI
	TableName string
}

func NewAppStoreDatabaseStore(api AppStoreTableAPI, tableName string) *AppStoreDatabaseStore {
	return &AppStoreDatabaseStore{api, tableName}
}

func (r *AppStoreDatabaseStore) GetBySourceUrl(ctx context.Context, sourceUrl string) ([]AppStoreApplication, error) {
	applications := []AppStoreApplication{}

	keyCondition := expression.Key("sourceUrl").Equal(expression.Value(sourceUrl))
	expr, err := expression.NewBuilder().WithKeyCondition(keyCondition).Build()
	if err != nil {
		return applications, fmt.Errorf("error building expression: %w", err)
	}

	response, err := r.api.Query(ctx, &dynamodb.QueryInput{
		TableName:                 aws.String(r.TableName),
		IndexName:                 aws.String("sourceUrl-index"),
		ExpressionAttributeNames:  expr.Names(),
		ExpressionAttributeValues: expr.Values(),
		KeyConditionExpression:    expr.KeyCondition(),
	})
	if err != nil {
		return applications, fmt.Errorf("error querying appstore applications by sourceUrl: %w", err)
	}

	err = attributevalue.UnmarshalListOfMaps(response.Items, &applications)
	if err != nil {
		return applications, fmt.Errorf("error unmarshaling appstore applications: %w", err)
	}

	return applications, nil
}

func (r *AppStoreDatabaseStore) GetAll(ctx context.Context) ([]AppStoreApplication, error) {
	applications := []AppStoreApplication{}

	response, err := r.api.Scan(ctx, &dynamodb.ScanInput{
		TableName: aws.String(r.TableName),
	})
	if err != nil {
		return applications, fmt.Errorf("error scanning appstore applications: %w", err)
	}

	err = attributevalue.UnmarshalListOfMaps(response.Items, &applications)
	if err != nil {
		return applications, fmt.Errorf("error unmarshaling appstore applications: %w", err)
	}

	return applications, nil
}

func (r *AppStoreDatabaseStore) Insert(ctx context.Context, application AppStoreApplication) error {
	item, err := attributevalue.MarshalMap(application)
	if err != nil {
		return fmt.Errorf("error marshaling appstore application: %w", err)
	}
	_, err = r.api.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(r.TableName), Item: item,
	})
	if err != nil {
		return fmt.Errorf("error inserting appstore application: %w", err)
	}

	return nil
}

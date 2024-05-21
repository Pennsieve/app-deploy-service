package store_dynamodb

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/expression"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go/aws"
)

type DynamoDBStore interface {
	Insert(context.Context, Application) error
	Get(context.Context, string, string) ([]Application, error)
	Delete(context.Context, string) error
}

type ApplicationDatabaseStore struct {
	DB        *dynamodb.Client
	TableName string
}

func NewApplicationDatabaseStore(db *dynamodb.Client, tableName string) DynamoDBStore {
	return &ApplicationDatabaseStore{db, tableName}
}

func (r *ApplicationDatabaseStore) Insert(ctx context.Context, application Application) error {
	item, err := attributevalue.MarshalMap(application)
	if err != nil {
		return fmt.Errorf("error marshaling application: %w", err)
	}
	_, err = r.DB.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(r.TableName), Item: item,
	})
	if err != nil {
		return fmt.Errorf("error inserting application: %w", err)
	}

	return nil
}

func (r *ApplicationDatabaseStore) Get(ctx context.Context, computeNodeUuid string, sourceUrl string) ([]Application, error) {
	applications := []Application{}
	filt1 := expression.Name("computeNodeUuid").Equal((expression.Value(computeNodeUuid)))
	filt2 := expression.Name("sourceUrl").Equal((expression.Value(sourceUrl)))
	expr, err := expression.NewBuilder().WithFilter(filt1.And(filt2)).Build()
	if err != nil {
		return applications, fmt.Errorf("error building expression: %w", err)
	}

	response, err := r.DB.Scan(ctx, &dynamodb.ScanInput{
		ExpressionAttributeNames:  expr.Names(),
		ExpressionAttributeValues: expr.Values(),
		FilterExpression:          expr.Filter(),
		ProjectionExpression:      expr.Projection(),
		TableName:                 aws.String(r.TableName),
	})
	if err != nil {
		return applications, fmt.Errorf("error getting applications: %w", err)
	}

	err = attributevalue.UnmarshalListOfMaps(response.Items, &applications)
	if err != nil {
		return applications, fmt.Errorf("error unmarshaling applications: %w", err)
	}

	return applications, nil
}

func (r *ApplicationDatabaseStore) Delete(ctx context.Context, applicationId string) error {
	key, err := attributevalue.MarshalMap(DeleteNode{Uuid: applicationId})
	if err != nil {
		return fmt.Errorf("error marshaling for delete: %w", err)
	}

	_, err = r.DB.DeleteItem(ctx, &dynamodb.DeleteItemInput{
		Key:       key,
		TableName: aws.String(r.TableName),
	})
	if err != nil {
		return fmt.Errorf("error deleting application: %w", err)
	}

	return nil
}

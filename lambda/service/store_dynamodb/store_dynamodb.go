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
	GetById(context.Context, string) (Application, error)
	Get(context.Context, string, map[string]string) ([]Application, error)
}

type ApplicationDatabaseStore struct {
	DB        *dynamodb.Client
	TableName string
}

func NewApplicationDatabaseStore(db *dynamodb.Client, tableName string) DynamoDBStore {
	return &ApplicationDatabaseStore{db, tableName}
}

func (r *ApplicationDatabaseStore) GetById(ctx context.Context, uuid string) (Application, error) {
	application := Application{Uuid: uuid}
	response, err := r.DB.GetItem(ctx, &dynamodb.GetItemInput{
		Key: application.GetKey(), TableName: aws.String(r.TableName),
	})
	if err != nil {
		return Application{}, fmt.Errorf("error getting application: %w", err)
	}
	if response.Item == nil {
		return Application{}, nil
	}

	err = attributevalue.UnmarshalMap(response.Item, &application)
	if err != nil {
		return application, fmt.Errorf("error unmarshaling application: %w", err)
	}

	return application, nil
}

func (r *ApplicationDatabaseStore) Get(ctx context.Context, organizationId string, params map[string]string) ([]Application, error) {
	applications := []Application{}

	var c expression.ConditionBuilder
	filt := expression.Name("organizationId").Equal((expression.Value(organizationId)))

	if applicationType, found := params["applicationType"]; found {
		c = filt.And(expression.Name("applicationType").Equal((expression.Value(applicationType))))
	}
	if computeNodeUuid, found := params["computeNodeUuid"]; found {
		c = filt.And(expression.Name("computeNodeUuid").Equal((expression.Value(computeNodeUuid))))
	}

	expr, err := expression.NewBuilder().WithFilter(c).Build()
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

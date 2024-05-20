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
	GetById(context.Context, string) (App, error)
	Get(context.Context, string) ([]App, error)
}

type NodeDatabaseStore struct {
	DB        *dynamodb.Client
	TableName string
}

func NewNodeDatabaseStore(db *dynamodb.Client, tableName string) DynamoDBStore {
	return &NodeDatabaseStore{db, tableName}
}
func (r *NodeDatabaseStore) GetById(ctx context.Context, uuid string) (App, error) {
	node := App{Uuid: uuid}
	response, err := r.DB.GetItem(ctx, &dynamodb.GetItemInput{
		Key: node.GetKey(), TableName: aws.String(r.TableName),
	})
	if err != nil {
		return App{}, fmt.Errorf("error getting node: %w", err)
	}
	if response.Item == nil {
		return App{}, nil
	}

	err = attributevalue.UnmarshalMap(response.Item, &node)
	if err != nil {
		return node, fmt.Errorf("error unmarshaling node: %w", err)
	}

	return node, nil
}

func (r *NodeDatabaseStore) Get(ctx context.Context, filter string) ([]App, error) {
	nodes := []App{}
	filt := expression.Name("organizationId").Equal((expression.Value(filter)))
	expr, err := expression.NewBuilder().WithFilter(filt).Build()
	if err != nil {
		return nodes, fmt.Errorf("error building expression: %w", err)
	}

	response, err := r.DB.Scan(ctx, &dynamodb.ScanInput{
		ExpressionAttributeNames:  expr.Names(),
		ExpressionAttributeValues: expr.Values(),
		FilterExpression:          expr.Filter(),
		ProjectionExpression:      expr.Projection(),
		TableName:                 aws.String(r.TableName),
	})
	if err != nil {
		return nodes, fmt.Errorf("error getting nodes: %w", err)
	}

	err = attributevalue.UnmarshalListOfMaps(response.Items, &nodes)
	if err != nil {
		return nodes, fmt.Errorf("error unmarshaling nodes: %w", err)
	}

	return nodes, nil
}

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

// DeploymentsTableAPI is an interface only containing the
// DynamoDB client methods used by DeploymentsStore
type DeploymentsTableAPI interface {
	UpdateItem(ctx context.Context, params *dynamodb.UpdateItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.UpdateItemOutput, error)
	BatchWriteItem(ctx context.Context, params *dynamodb.BatchWriteItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.BatchWriteItemOutput, error)
	Query(ctx context.Context, params *dynamodb.QueryInput, optFns ...func(options *dynamodb.Options)) (*dynamodb.QueryOutput, error)
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

func (s *DeploymentsStore) SetErroredFlag(ctx context.Context, applicationId string, deploymentId string) error {
	key, err := attributevalue.MarshalMap(DeploymentKey{
		ApplicationId: applicationId,
		DeploymentId:  deploymentId,
	})
	if err != nil {
		return fmt.Errorf("error marshaling key for deployment %s 'errored' flag update: %w", deploymentId, err)
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
		return fmt.Errorf("error updating 'errored' flag on deployment %s: %w", deploymentId, err)
	}

	return nil
}

func (s *DeploymentsStore) DeleteApplicationDeployments(ctx context.Context, applicationId string) error {
	expressions, err := expression.NewBuilder().
		WithKeyCondition(expression.KeyEqual(
			expression.Key(DeploymentApplicationIdField), expression.Value(applicationId))).
		WithProjection(expression.NamesList(expression.Name(DeploymentIdField), expression.Name(DeploymentApplicationIdField))).
		Build()
	if err != nil {
		return fmt.Errorf("error building key condition for query to delete deployments of application %s: %w", applicationId, err)
	}
	queryIn := &dynamodb.QueryInput{
		TableName:                 aws.String(s.tableName),
		ExpressionAttributeNames:  expressions.Names(),
		ExpressionAttributeValues: expressions.Values(),
		KeyConditionExpression:    expressions.KeyCondition(),
		ProjectionExpression:      expressions.Projection(),
		Limit:                     aws.Int32(25), // 25 is max number of items that can be deleted in a batch delete
	}

	for doQuery, page := true, 1; doQuery; doQuery, page = len(queryIn.ExclusiveStartKey) > 0, page+1 {
		// Get a batch of items
		queryOut, err := s.api.Query(ctx, queryIn)
		if err != nil {
			return fmt.Errorf("error getting page %d of deployments to delete for application %s: %w", page, applicationId, err)
		}
		// Delete this batch of items
		deleteBatch := batchDeletes(s.tableName, queryOut.Items)
		for len(deleteBatch) > 0 {
			batchWriteOut, err := s.api.BatchWriteItem(ctx, &dynamodb.BatchWriteItemInput{RequestItems: deleteBatch})
			if err != nil {
				return fmt.Errorf("error deleting page %d of deployments for application %s: %w", page, applicationId, err)
			}
			deleteBatch = batchWriteOut.UnprocessedItems
		}
		queryIn.ExclusiveStartKey = queryOut.LastEvaluatedKey
	}

	return nil
}

func batchDeletes(tableName string, items []map[string]types.AttributeValue) map[string][]types.WriteRequest {
	var deleteBatch []types.WriteRequest
	for _, item := range items {
		deleteBatch = append(deleteBatch, types.WriteRequest{
			DeleteRequest: &types.DeleteRequest{Key: item},
		})
	}
	return map[string][]types.WriteRequest{tableName: deleteBatch}
}

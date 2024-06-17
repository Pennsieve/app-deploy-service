package store_dynamodb

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/expression"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/aws-sdk-go/aws"
)

type DynamoDBStore interface {
	Update(context.Context, Application, string) error
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

func (r *ApplicationDatabaseStore) Update(ctx context.Context, application Application, applicationUuid string) error {
	key, err := attributevalue.MarshalMap(ApplicationKey{Uuid: applicationUuid})
	if err != nil {
		return fmt.Errorf("error marshaling key for update: %w", err)
	}

	_, err = r.DB.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName: aws.String(r.TableName),
		Key:       key,
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":i": &types.AttributeValueMemberS{Value: application.ApplicationId},
			":c": &types.AttributeValueMemberS{Value: application.ApplicationContainerName},
			":d": &types.AttributeValueMemberS{Value: application.DestinationUrl},
			":s": &types.AttributeValueMemberS{Value: application.Status},
		},
		UpdateExpression: aws.String("set applicationId = :i, applicationContainerName = :c, destinationUrl = :d, registrationStatus = :s"),
	})
	if err != nil {
		return fmt.Errorf("error updating application: %w", err)
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

func (r *ApplicationDatabaseStore) Delete(ctx context.Context, applicationUuid string) error {
	key, err := attributevalue.MarshalMap(ApplicationKey{Uuid: applicationUuid})
	if err != nil {
		return fmt.Errorf("error marshaling key for delete: %w", err)
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

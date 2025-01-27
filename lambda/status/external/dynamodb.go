package external

import (
	"context"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
)

type DynamoDBApi interface {
	UpdateItem(ctx context.Context, params *dynamodb.UpdateItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.UpdateItemOutput, error)
}

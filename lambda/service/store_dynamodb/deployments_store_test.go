package store_dynamodb

import (
	"context"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
)

type ArgCaptureDeploymentsTableAPI struct {
	putItemInput    *dynamodb.PutItemInput
	updateItemInput *dynamodb.UpdateItemInput
	getItemInput    *dynamodb.GetItemInput
}

func (a *ArgCaptureDeploymentsTableAPI) PutItem(_ context.Context, params *dynamodb.PutItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error) {
	a.putItemInput = params
	return &dynamodb.PutItemOutput{}, nil
}

func (a *ArgCaptureDeploymentsTableAPI) UpdateItem(_ context.Context, params *dynamodb.UpdateItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.UpdateItemOutput, error) {
	a.updateItemInput = params
	return &dynamodb.UpdateItemOutput{}, nil
}

func (a *ArgCaptureDeploymentsTableAPI) GetItem(_ context.Context, params *dynamodb.GetItemInput, _ ...func(options *dynamodb.Options)) (*dynamodb.GetItemOutput, error) {
	a.getItemInput = params
	return &dynamodb.GetItemOutput{}, nil
}

func TestDeploymentsStore_Get(t *testing.T) {
	argCaptureAPI := new(ArgCaptureDeploymentsTableAPI)
	tableName := uuid.NewString()
	store := NewDeploymentsStore(argCaptureAPI, tableName)
	applicationId := uuid.NewString()
	deploymentId := uuid.NewString()
	deployment, err := store.Get(context.Background(), applicationId, deploymentId)
	require.NoError(t, err)
	assert.Nil(t, deployment)

	getItemInput := argCaptureAPI.getItemInput
	assert.Equal(t, tableName, aws.ToString(getItemInput.TableName))
	expectedKey := map[string]types.AttributeValue{
		DeploymentApplicationIdField: &types.AttributeValueMemberS{Value: applicationId},
		DeploymentIdField:            &types.AttributeValueMemberS{Value: deploymentId},
	}
	assert.Equal(t, expectedKey, getItemInput.Key)
}

package store_dynamodb

import (
	"context"
	"fmt"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
)

type ArgCaptureDeploymentsTableAPI struct {
	// Don't save pointers to inputs; make defensive copies instead

	PutItemInput    dynamodb.PutItemInput
	UpdateItemInput dynamodb.UpdateItemInput
	GetItemInput    dynamodb.GetItemInput
	// Query may be paginated, so one GetHistory call may result in multiple Query calls.

	// Set QueryOutputs before calling GetHistory to control how many times Query is called.
	QueryOutputs            []*dynamodb.QueryOutput
	currentQueryOutputIndex int
	// Check QueryInputs after calling GetHistory to see if we sent the correct inputs.
	QueryInputs []dynamodb.QueryInput
}

func (a *ArgCaptureDeploymentsTableAPI) PutItem(_ context.Context, params *dynamodb.PutItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error) {
	a.PutItemInput = *params
	return &dynamodb.PutItemOutput{}, nil
}

func (a *ArgCaptureDeploymentsTableAPI) UpdateItem(_ context.Context, params *dynamodb.UpdateItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.UpdateItemOutput, error) {
	a.UpdateItemInput = *params
	return &dynamodb.UpdateItemOutput{}, nil
}

func (a *ArgCaptureDeploymentsTableAPI) GetItem(_ context.Context, params *dynamodb.GetItemInput, _ ...func(options *dynamodb.Options)) (*dynamodb.GetItemOutput, error) {
	a.GetItemInput = *params
	return &dynamodb.GetItemOutput{}, nil
}

func (a *ArgCaptureDeploymentsTableAPI) Query(_ context.Context, params *dynamodb.QueryInput, _ ...func(options *dynamodb.Options)) (*dynamodb.QueryOutput, error) {
	a.QueryInputs = append(a.QueryInputs, *params)
	if a.currentQueryOutputIndex < len(a.QueryOutputs) {
		output := a.QueryOutputs[a.currentQueryOutputIndex]
		a.currentQueryOutputIndex++
		return output, nil
	}
	return nil, fmt.Errorf("query called too many times! Expected %d calls", len(a.QueryOutputs))

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

	getItemInput := argCaptureAPI.GetItemInput
	assert.Equal(t, tableName, aws.ToString(getItemInput.TableName))
	expectedKey := deploymentKeyItem(applicationId, deploymentId)
	assert.Equal(t, expectedKey, getItemInput.Key)
}

func TestDeploymentsStore_GetHistory(t *testing.T) {
	argCaptureAPI := new(ArgCaptureDeploymentsTableAPI)
	tableName := uuid.NewString()
	applicationId := uuid.NewString()
	expectedDeploymentItems := []map[string]types.AttributeValue{
		deploymentKeyItem(applicationId, uuid.NewString()),
		deploymentKeyItem(applicationId, uuid.NewString()),
		deploymentKeyItem(applicationId, uuid.NewString()),
	}
	argCaptureAPI.QueryOutputs = []*dynamodb.QueryOutput{
		{
			Count:            1,
			Items:            []map[string]types.AttributeValue{expectedDeploymentItems[0]},
			LastEvaluatedKey: expectedDeploymentItems[0],
			ScannedCount:     1,
		},
		{
			Count:            1,
			Items:            []map[string]types.AttributeValue{expectedDeploymentItems[1]},
			LastEvaluatedKey: expectedDeploymentItems[1],
			ScannedCount:     1,
		},
		{
			Count:            1,
			Items:            []map[string]types.AttributeValue{expectedDeploymentItems[2]},
			LastEvaluatedKey: nil,
			ScannedCount:     1,
		},
	}
	store := NewDeploymentsStore(argCaptureAPI, tableName)
	deployments, err := store.GetHistory(context.Background(), applicationId)
	require.NoError(t, err)
	assert.Len(t, deployments, len(expectedDeploymentItems))
	var expectedDeployments []Deployment
	expectedDeployments, err = fromItems(expectedDeploymentItems, expectedDeployments)
	require.NoError(t, err)
	assert.Equal(t, expectedDeployments, deployments)

	for i := range argCaptureAPI.QueryInputs {
		input := argCaptureAPI.QueryInputs[i]
		assert.Equal(t, tableName, aws.ToString(input.TableName))
		if i == 0 {
			assert.Empty(t, input.ExclusiveStartKey)
		} else {
			assert.Equal(t, expectedDeploymentItems[i-1], input.ExclusiveStartKey)
		}
		// Names
		assert.Len(t, input.ExpressionAttributeNames, 1)
		var appIdNameKey string
		for k, v := range input.ExpressionAttributeNames {
			if assert.Equal(t, DeploymentApplicationIdField, v) {
				appIdNameKey = k
			}
		}

		//Values
		var appIdValueKey string
		for k, v := range input.ExpressionAttributeValues {
			if assert.Equal(t, &types.AttributeValueMemberS{Value: applicationId}, v) {
				appIdValueKey = k
			}
		}

		assert.Equal(t, fmt.Sprintf("%s = %s", appIdNameKey, appIdValueKey), aws.ToString(input.KeyConditionExpression))

	}
}

func deploymentKeyItem(applicationId, deploymentId string) map[string]types.AttributeValue {
	return map[string]types.AttributeValue{
		DeploymentApplicationIdField: &types.AttributeValueMemberS{Value: applicationId},
		DeploymentIdField:            &types.AttributeValueMemberS{Value: deploymentId},
	}
}

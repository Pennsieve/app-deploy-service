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
	UpdateItemInput      dynamodb.UpdateItemInput
	BatchWriteItemInputs []dynamodb.BatchWriteItemInput

	// Query may be paginated, so one DeleteApplicationDeployments call may result in multiple Query calls.
	// Set QueryOutputs before calling DeleteApplicationDeployments to control how many times Query is called.
	QueryOutputs            []*dynamodb.QueryOutput
	currentQueryOutputIndex int
	// Check QueryInputs after calling DeleteApplicationDeployments to see if we sent the correct inputs.
	QueryInputs []dynamodb.QueryInput
}

func (a *ArgCaptureDeploymentsTableAPI) BatchWriteItem(_ context.Context, params *dynamodb.BatchWriteItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.BatchWriteItemOutput, error) {
	a.BatchWriteItemInputs = append(a.BatchWriteItemInputs, *params)
	return &dynamodb.BatchWriteItemOutput{}, nil
}

func (a *ArgCaptureDeploymentsTableAPI) UpdateItem(_ context.Context, params *dynamodb.UpdateItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.UpdateItemOutput, error) {
	a.UpdateItemInput = *params
	return &dynamodb.UpdateItemOutput{}, nil
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

func TestDeploymentsStore_DeleteApplicationDeployments(t *testing.T) {
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
	err := store.DeleteApplicationDeployments(context.Background(), applicationId)
	require.NoError(t, err)

	assert.Len(t, argCaptureAPI.QueryInputs, len(expectedDeploymentItems))

	for i := range argCaptureAPI.QueryInputs {
		input := argCaptureAPI.QueryInputs[i]
		assert.Equal(t, tableName, aws.ToString(input.TableName))
		if i == 0 {
			assert.Empty(t, input.ExclusiveStartKey)
		} else {
			assert.Equal(t, expectedDeploymentItems[i-1], input.ExclusiveStartKey)
		}
		// Names
		assert.Len(t, input.ExpressionAttributeNames, 2)
		var deploymentIdNameKey, appIdNameKey string
		for k, v := range input.ExpressionAttributeNames {
			if DeploymentApplicationIdField == v {
				appIdNameKey = k
			} else if DeploymentIdField == v {
				deploymentIdNameKey = k
			} else {
				assert.Fail(t, "unexpected value in ExpressionAttributeNames", v)
			}
		}
		assert.NotEmpty(t, appIdNameKey)
		assert.NotEmpty(t, deploymentIdNameKey)

		//Values
		var appIdValueKey string
		for k, v := range input.ExpressionAttributeValues {
			if assert.Equal(t, &types.AttributeValueMemberS{Value: applicationId}, v) {
				appIdValueKey = k
			}
		}
		assert.NotEmpty(t, appIdValueKey)

		//Key expression
		assert.Equal(t, fmt.Sprintf("%s = %s", appIdNameKey, appIdValueKey), aws.ToString(input.KeyConditionExpression))

		//Projection expression
		assert.Equal(t, fmt.Sprintf("%s, %s", deploymentIdNameKey, appIdNameKey), aws.ToString(input.ProjectionExpression))

	}
}

func deploymentKeyItem(applicationId, deploymentId string) map[string]types.AttributeValue {
	return map[string]types.AttributeValue{
		DeploymentApplicationIdField: &types.AttributeValueMemberS{Value: applicationId},
		DeploymentIdField:            &types.AttributeValueMemberS{Value: deploymentId},
	}
}

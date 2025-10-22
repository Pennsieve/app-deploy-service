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
	// If there are > UnprocessedItemThreshold DeleteRequests in a BatchWriteItemInput
	// this mock API will return the remainder in the UnprocessedItems field of the corresponding BatchWriteItemOutput
	UnprocessedItemThreshold int

	// Query may be paginated, so one DeleteApplicationDeployments call may result in multiple Query calls.
	// Set QueryOutputs before calling DeleteApplicationDeployments to control how many times Query is called.
	QueryOutputs            []*dynamodb.QueryOutput
	currentQueryOutputIndex int
	// Check QueryInputs after calling DeleteApplicationDeployments to see if we sent the correct inputs.
	QueryInputs []dynamodb.QueryInput
}

func (a *ArgCaptureDeploymentsTableAPI) BatchWriteItem(_ context.Context, params *dynamodb.BatchWriteItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.BatchWriteItemOutput, error) {
	a.BatchWriteItemInputs = append(a.BatchWriteItemInputs, *params)
	unprocessed := map[string][]types.WriteRequest{}
	for k, v := range params.RequestItems {
		if len(v) > a.UnprocessedItemThreshold {
			unprocessed[k] = v[a.UnprocessedItemThreshold:]
		}
	}
	return &dynamodb.BatchWriteItemOutput{UnprocessedItems: unprocessed}, nil
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
	unprocessedItemThreshold := 3
	argCaptureAPI := &ArgCaptureDeploymentsTableAPI{UnprocessedItemThreshold: unprocessedItemThreshold}
	tableName := uuid.NewString()
	applicationId := uuid.NewString()
	expectedDeploymentItems := []map[string]types.AttributeValue{
		deploymentKeyItem(applicationId, uuid.NewString()),
		deploymentKeyItem(applicationId, uuid.NewString()),
		deploymentKeyItem(applicationId, uuid.NewString()),
		deploymentKeyItem(applicationId, uuid.NewString()),
		deploymentKeyItem(applicationId, uuid.NewString()),
		deploymentKeyItem(applicationId, uuid.NewString()),
	}
	argCaptureAPI.QueryOutputs = []*dynamodb.QueryOutput{
		{
			Count:            5,
			Items:            expectedDeploymentItems[:5],
			LastEvaluatedKey: expectedDeploymentItems[4],
			ScannedCount:     5,
		},
		{
			Count:            1,
			Items:            []map[string]types.AttributeValue{expectedDeploymentItems[5]},
			LastEvaluatedKey: nil,
			ScannedCount:     1,
		},
	}
	store := NewDeploymentsStore(argCaptureAPI, tableName)
	err := store.DeleteApplicationDeployments(context.Background(), applicationId)
	require.NoError(t, err)

	// Verify Query Inputs
	for i := range argCaptureAPI.QueryInputs {
		input := argCaptureAPI.QueryInputs[i]
		assert.Equal(t, tableName, aws.ToString(input.TableName))
		assert.Equal(t, int32(25), aws.ToInt32(input.Limit))
		if i == 0 {
			assert.Empty(t, input.ExclusiveStartKey)
		} else {
			assert.Equal(t, argCaptureAPI.QueryOutputs[i-1].LastEvaluatedKey, input.ExclusiveStartKey)
		}
		// Names
		assert.Len(t, input.ExpressionAttributeNames, 2)
		var deploymentIdNameKey, appIdNameKey string
		for k, v := range input.ExpressionAttributeNames {
			switch v {
			case DeploymentApplicationIdField:
				appIdNameKey = k
			case DeploymentIdField:
				deploymentIdNameKey = k
			default:
				assert.Fail(t, "unexpected value in ExpressionAttributeNames", v)

			}
		}
		assert.NotEmpty(t, appIdNameKey)
		assert.NotEmpty(t, deploymentIdNameKey)

		//Values
		assert.Len(t, input.ExpressionAttributeValues, 1)
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

	// Verify BatchWrite Inputs
	// The two query outputs will turn into three batch write inputs because of how we have
	// UnprocessedItemThreshold configured on the mock
	expectedDeleteRequests := [][]map[string]types.AttributeValue{
		// First batch write will attempt to delete all 5 of the items returned by first QueryOutput
		expectedDeploymentItems[:5],
		// There will be a second batch write to process the unprocessed items from the first
		// batch write request
		expectedDeploymentItems[unprocessedItemThreshold : len(expectedDeploymentItems)-1],
		// Final batch write in response to second QueryOutput
		{expectedDeploymentItems[len(expectedDeploymentItems)-1]},
	}
	assert.Len(t, argCaptureAPI.BatchWriteItemInputs, len(expectedDeleteRequests))
	for i := range argCaptureAPI.BatchWriteItemInputs {
		input := argCaptureAPI.BatchWriteItemInputs[i]
		assert.Len(t, input.RequestItems, 1)
		assert.Contains(t, input.RequestItems, tableName)
		writeRequests := input.RequestItems[tableName]
		assert.Len(t, writeRequests, len(expectedDeleteRequests[i]))
		for j := 0; j < len(expectedDeleteRequests[i]); j++ {
			assert.Nil(t, writeRequests[j].PutRequest)
			assert.Equal(t, expectedDeleteRequests[i][j], writeRequests[j].DeleteRequest.Key)
		}
	}

}

func deploymentKeyItem(applicationId, deploymentId string) map[string]types.AttributeValue {
	return map[string]types.AttributeValue{
		DeploymentApplicationIdField: &types.AttributeValueMemberS{Value: applicationId},
		DeploymentIdField:            &types.AttributeValueMemberS{Value: deploymentId},
	}
}

package handler

import (
	"context"
	"fmt"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/expression"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/google/uuid"
	"github.com/pennsieve/app-deploy-service/status/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

type ArgCaptureDynamoDBApi struct {
	UpdateItemIn *dynamodb.UpdateItemInput
	GetItemIn    *dynamodb.GetItemInput
}

func (a *ArgCaptureDynamoDBApi) GetItem(_ context.Context, params *dynamodb.GetItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error) {
	a.GetItemIn = params
	return &dynamodb.GetItemOutput{}, nil
}

func (a *ArgCaptureDynamoDBApi) UpdateItem(_ context.Context, params *dynamodb.UpdateItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.UpdateItemOutput, error) {
	a.UpdateItemIn = params
	return &dynamodb.UpdateItemOutput{}, nil
}

func TestDeployTaskStateChangeHandler_UpdateApplicationsTable_Expressions(t *testing.T) {
	argCaptureDynamo := new(ArgCaptureDynamoDBApi)
	applicationsTable := uuid.NewString()
	deploymentsTable := uuid.NewString()
	handler := NewDeployTaskStateChangeHandler(nil, argCaptureDynamo, applicationsTable, deploymentsTable)

	applicationId := uuid.NewString()
	finalState := &FinalState{}

	err := handler.UpdateApplicationsTable(context.Background(), applicationId, finalState)
	require.NoError(t, err)

	updateItemIn := argCaptureDynamo.UpdateItemIn

	assert.Equal(t, models.ApplicationKey(applicationId), updateItemIn.Key)
	assert.Equal(t, applicationsTable, aws.ToString(updateItemIn.TableName))

	actualNames := updateItemIn.ExpressionAttributeNames
	assert.Len(t, actualNames, 2)
	var uuidName, statusName string
	for k, v := range actualNames {
		if v == models.ApplicationKeyField {
			uuidName = k
		} else if v == models.ApplicationStatusField {
			statusName = k
		}
	}
	assert.NotEmpty(t, uuidName)
	assert.NotEmpty(t, statusName)

	actualCondition := *updateItemIn.ConditionExpression
	assert.Equal(t, fmt.Sprintf("attribute_exists (%s)", uuidName), actualCondition)

	actualValues := updateItemIn.ExpressionAttributeValues
	assert.Len(t, actualValues, 1)
	var statusValueName string
	for k, v := range actualValues {
		statusValueName = k
		statusav, typeCorrect := v.(*types.AttributeValueMemberS)
		if assert.True(t, typeCorrect) {
			actualStatus := statusav.Value
			assert.Equal(t, finalState.Status(), actualStatus)
		}
	}
	actualUpdate := *updateItemIn.UpdateExpression
	assert.Equal(t, fmt.Sprintf("SET %s = %s\n", statusName, statusValueName), actualUpdate)
}

func TestDeploymentUpdateBuilder(t *testing.T) {
	// Not really a test, just debugging
	event := models.TaskStateChangeEvent{
		Id:         "",
		Version:    "",
		Time:       time.Time{},
		DetailType: "",
		Region:     "",
		Resources:  nil,
		Source:     "",
		Account:    "",
		Detail:     models.Detail{},
	}
	updateBuilder := DeploymentUpdateBuilder(event)

	updateExpression, err := expression.NewBuilder().WithUpdate(updateBuilder).Build()
	require.NoError(t, err)
	fmt.Println(updateExpression)
}

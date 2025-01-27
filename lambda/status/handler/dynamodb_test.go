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
	"strings"
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

func TestDeploymentUpdateBuilder_PartialUpdate(t *testing.T) {
	createdAt := time.Now().UTC()
	event := models.TaskStateChangeEvent{
		Detail: models.Detail{
			LastStatus:    "PENDING",
			DesiredStatus: "RUNNING",
			TaskArn:       uuid.NewString(),
			Version:       1,
			CreatedAt:     &createdAt,
			StartedAt:     nil,
			UpdatedAt:     nil,
			StoppedAt:     nil,
			StopCode:      "",
			StoppedReason: "",
		},
	}
	updateBuilder := DeploymentUpdateBuilder(event)

	update, err := expression.NewBuilder().WithUpdate(updateBuilder).Build()
	require.NoError(t, err)
	// First 4 are always expected.
	// Last 2 are only expected because the values in event.Detail are
	// non-nil/non-empty
	expectedNames := []string{
		models.DeploymentTaskArnField,
		models.DeploymentVersionField,
		models.DeploymentLastStatusField,
		models.DeploymentDesiredStatusField,
		models.DeploymentCreatedAtField,
	}
	assert.Len(t, update.Names(), len(expectedNames))

	for _, expectedName := range expectedNames {
		found := false
		for _, actualName := range update.Names() {
			if actualName == expectedName {
				found = true
			}
		}
		assert.True(t, found, "update expression names missing expected name: %s", expectedName)
	}

	updateExpression := aws.ToString(update.Update())
	assert.True(t, strings.HasPrefix(updateExpression, "SET"))
	assert.Equal(t, len(expectedNames), strings.Count(updateExpression, "="))
}

func TestDeploymentUpdateBuilder_FullUpdate(t *testing.T) {
	createdAt := time.Now().UTC()
	startedAt := createdAt.Add(1 * time.Minute)
	updatedAt := startedAt.Add(30 * time.Second)
	detail := models.Detail{
		LastStatus:    models.StateStopped,
		DesiredStatus: models.StateStopped,
		TaskArn:       uuid.NewString(),
		Version:       6,
		CreatedAt:     &createdAt,
		StartedAt:     &startedAt,
		UpdatedAt:     &updatedAt,
		StoppedAt:     &updatedAt,
		StopCode:      uuid.NewString(),
		StoppedReason: uuid.NewString(),
	}

	for scenario, exitCode := range map[string]int{
		"no error": 0,
		"error":    1,
	} {
		t.Run(scenario, func(t *testing.T) {
			detail.Containers = []models.Container{
				{ExitCode: exitCode},
			}
			event := models.TaskStateChangeEvent{
				Detail: detail,
			}
			updateBuilder := DeploymentUpdateBuilder(event)

			update, err := expression.NewBuilder().WithUpdate(updateBuilder).Build()
			require.NoError(t, err)
			// First 4 are always expected.
			// Remaining are expected because the values in event.Detail are
			// non-nil/non-empty
			expectedNames := []string{
				models.DeploymentTaskArnField,
				models.DeploymentVersionField,
				models.DeploymentLastStatusField,
				models.DeploymentDesiredStatusField,
				models.DeploymentCreatedAtField,
				models.DeploymentStartedAtField,
				models.DeploymentUpdatedAtField,
				models.DeploymentStoppedAtField,
				models.DeploymentStopCodeField,
				models.DeploymentStoppedReasonField,
				models.DeploymentErroredField,
			}
			assert.Len(t, update.Names(), len(expectedNames))
			var errorAlias string

			for _, expectedName := range expectedNames {
				found := false
				for actualAlias, actualName := range update.Names() {
					if actualName == expectedName {
						found = true
					}
					if actualName == models.DeploymentErroredField {
						errorAlias = actualAlias
					}
				}
				assert.True(t, found, "update expression names missing expected name: %s", expectedName)
			}

			updateExpression := aws.ToString(update.Update())
			assert.True(t, strings.HasPrefix(updateExpression, "SET"))
			assert.Equal(t, len(expectedNames), strings.Count(updateExpression, "="))

			assert.NotEmpty(t, errorAlias)
			errorValueAlias := strings.ReplaceAll(errorAlias, "#", ":")
			// checking assumption that the value alias is the same as the name alias with '#' replaced by ':'
			assert.Contains(t, updateExpression, fmt.Sprintf("%s = %s", errorAlias, errorValueAlias))

			updateValues := update.Values()
			assert.Contains(t, updateValues, errorValueAlias)

			errorValueAV := updateValues[errorValueAlias]
			errorValueAVBOOL, isBoolean := errorValueAV.(*types.AttributeValueMemberBOOL)
			assert.True(t, isBoolean)
			assert.Equal(t, exitCode != 0, errorValueAVBOOL.Value)
		})
	}

}

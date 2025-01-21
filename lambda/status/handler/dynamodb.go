package handler

import (
	"context"
	"errors"
	"fmt"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/expression"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/pennsieve/app-deploy-service/status/dydbutils"
	"github.com/pennsieve/app-deploy-service/status/models"
)

func (h *DeployTaskStateChangeHandler) UpdateDeploymentsTable(ctx context.Context, deploymentId string, event models.TaskStateChangeEvent) error {
	key := models.DeploymentKey(deploymentId)

	updateBuilder := DeploymentUpdateBuilder(event)

	expressions, err := expression.NewBuilder().
		WithCondition(
			expression.AttributeExists(expression.Name(models.DeploymentKeyField)).
				And(expression.Or(
					expression.AttributeNotExists(expression.Name(models.DeploymentVersionField)),
					expression.LessThan(expression.Name(models.DeploymentVersionField), expression.Value(event.Detail.Version))))).
		WithUpdate(updateBuilder).
		Build()
	if err != nil {
		return fmt.Errorf("error building deployments table update expressions for deployment %s: %w",
			deploymentId,
			err)
	}

	updateIn := &dynamodb.UpdateItemInput{
		Key:                                 key,
		TableName:                           aws.String(h.DeploymentsTable),
		ConditionExpression:                 expressions.Condition(),
		ExpressionAttributeNames:            expressions.Names(),
		ExpressionAttributeValues:           expressions.Values(),
		ReturnValuesOnConditionCheckFailure: types.ReturnValuesOnConditionCheckFailureAllOld,
		UpdateExpression:                    expressions.Update(),
	}
	if _, err = h.DynamoDBApi.UpdateItem(ctx, updateIn); err == nil {
		return nil
	}
	var conditionFailedError *types.ConditionalCheckFailedException
	if errors.As(err, &conditionFailedError) {
		conflict := &DeploymentUpdateConflict{}
		if existingRecord, err := dydbutils.FromItem[models.Deployment](conditionFailedError.Item); err == nil {
			conflict.Existing = existingRecord
		} else {
			conflict.UnmarshallingError = err
		}
		return conflict
	}
	return fmt.Errorf("error updating deployment %s: %w",
		deploymentId,
		err)
}

func DeploymentUpdateBuilder(event models.TaskStateChangeEvent) expression.UpdateBuilder {
	detail := event.Detail
	updateExpressionBuilder :=
		expression.Set(expression.Name(models.DeploymentTaskArnField), expression.Value(detail.TaskArn)).
			Set(expression.Name(models.DeploymentVersionField), expression.Value(detail.Version)).
			Set(expression.Name(models.DeploymentLastStatusField), expression.Value(detail.LastStatus)).
			Set(expression.Name(models.DeploymentDesiredStatusField), expression.Value(detail.DesiredStatus))
	return updateExpressionBuilder
}

func (h *DeployTaskStateChangeHandler) UpdateApplicationsTable(ctx context.Context, applicationId string, finalState *FinalState) error {
	key := models.ApplicationKey(applicationId)
	status := finalState.Status()
	expressions, err := expression.NewBuilder().
		WithCondition(expression.AttributeExists(expression.Name(models.ApplicationKeyField))).
		WithUpdate(expression.Set(expression.Name(models.ApplicationStatusField), expression.Value(status))).Build()
	if err != nil {
		return fmt.Errorf("error building applications table update expression for application %s: %w",
			applicationId,
			err)
	}
	updateIn := &dynamodb.UpdateItemInput{
		Key:                       key,
		TableName:                 aws.String(h.ApplicationsTable),
		ConditionExpression:       expressions.Condition(),
		ExpressionAttributeNames:  expressions.Names(),
		ExpressionAttributeValues: expressions.Values(),
		UpdateExpression:          expressions.Update(),
	}
	if _, err := h.DynamoDBApi.UpdateItem(ctx, updateIn); err != nil {
		return fmt.Errorf("error updating application %s in table %s to status: %s: %w",
			applicationId,
			h.ApplicationsTable,
			status,
			err)
	}
	return nil
}

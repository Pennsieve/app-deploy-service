package handler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/expression"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/pennsieve/app-deploy-service/status/dydbutils"
	"github.com/pennsieve/app-deploy-service/status/models"
)

func (h *DeployTaskStateChangeHandler) UpdateDeploymentsTable(ctx context.Context, applicationId, deploymentId string, event models.TaskStateChangeEvent) error {
	key := models.DeploymentKeyItem(applicationId, deploymentId)

	updateBuilder := DeploymentUpdateBuilder(event)

	// Condition: Only update if item already exists (no upsert) and if
	// this is first update (no version value exists yet) or existing version is less than our version
	expressions, err := expression.NewBuilder().
		WithCondition(
			expression.AttributeExists(expression.Name(models.DeploymentApplicationIdField)).And(
				expression.AttributeExists(expression.Name(models.DeploymentIdField))).
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
	builder := expression.Set(expression.Name(models.DeploymentTaskArnField), expression.Value(detail.TaskArn)).
		Set(expression.Name(models.DeploymentVersionField), expression.Value(detail.Version)).
		Set(expression.Name(models.DeploymentLastStatusField), expression.Value(detail.LastStatus)).
		Set(expression.Name(models.DeploymentDesiredStatusField), expression.Value(detail.DesiredStatus))

	setOptionalTime(builder, models.DeploymentUpdatedAtField, detail.UpdatedAt)
	setOptionalTime(builder, models.DeploymentCreatedAtField, detail.CreatedAt)
	setOptionalTime(builder, models.DeploymentStartedAtField, detail.StartedAt)
	setOptionalTime(builder, models.DeploymentStoppedAtField, detail.StoppedAt)

	setOptionalString(builder, models.DeploymentStopCodeField, detail.StopCode)
	setOptionalString(builder, models.DeploymentStoppedReasonField, detail.StoppedReason)

	if finalState := IsFinalState(event); finalState != nil {
		builder.Set(expression.Name(models.DeploymentErroredField), expression.Value(finalState.Errored))
	}
	return builder
}

func setOptionalTime(updateBuilder expression.UpdateBuilder, name string, optionalTime *time.Time) {
	if optionalTime != nil {
		updateBuilder.Set(expression.Name(name), expression.Value(*optionalTime))
	}
}

func setOptionalString(updateBuilder expression.UpdateBuilder, name string, optionalValue string) {
	if len(optionalValue) > 0 {
		updateBuilder.Set(expression.Name(name), expression.Value(optionalValue))
	}
}

// UpdateVariantStatus records how a variant's build ended on the version:
// variants.<id>.status, and failureReason when it failed. The variant must
// already be recorded (the service lambda writes every variant at
// registration).
func (h *DeployTaskStateChangeHandler) UpdateVariantStatus(ctx context.Context, applicationId, variant string, finalState *FinalState, failureReason string, tableName string) error {
	in, err := VariantStatusUpdate(applicationId, variant, finalState, failureReason, tableName)
	if err != nil {
		return err
	}
	if _, err := h.DynamoDBApi.UpdateItem(ctx, in); err != nil {
		var notRecorded *types.ConditionalCheckFailedException
		if errors.As(err, &notRecorded) {
			// nothing to update; retrying won't change that
			h.logger.Warn("variant not recorded on the version; its status is not kept",
				slog.String("variant", variant), slog.String("status", finalState.Status()))
			return nil
		}
		return fmt.Errorf("error updating variant %s of %s in table %s to status %s: %w",
			variant, applicationId, tableName, finalState.Status(), err)
	}
	return nil
}

// VariantStatusUpdate is the update UpdateVariantStatus makes.
func VariantStatusUpdate(applicationId, variant string, finalState *FinalState, failureReason string, tableName string) (*dynamodb.UpdateItemInput, error) {
	path := func(field string) expression.NameBuilder {
		return expression.Name(models.VariantsField + "." + variant + "." + field)
	}
	update := expression.Set(path(models.VariantStatusField), expression.Value(finalState.Status()))
	if finalState.Errored {
		if failureReason == "" {
			failureReason = "image build failed"
		}
		update = update.Set(path(models.VariantFailureReasonField), expression.Value(failureReason))
	} else {
		update = update.Remove(path(models.VariantFailureReasonField))
	}
	expressions, err := expression.NewBuilder().
		WithCondition(expression.AttributeExists(expression.Name(models.VariantsField + "." + variant))).
		WithUpdate(update).Build()
	if err != nil {
		return nil, fmt.Errorf("error building the status update of variant %s of %s: %w", variant, applicationId, err)
	}
	return &dynamodb.UpdateItemInput{
		Key:                       models.ApplicationKey(applicationId),
		TableName:                 aws.String(tableName),
		ConditionExpression:       expressions.Condition(),
		ExpressionAttributeNames:  expressions.Names(),
		ExpressionAttributeValues: expressions.Values(),
		UpdateExpression:          expressions.Update(),
	}, nil
}

// VariantFailureReason is why a variant's build failed, for its
// failureReason: the build's stopped reason (for CodeBuild, its failed
// phase, log tail and log link), with the kaniko container's exit code.
func VariantFailureReason(event models.TaskStateChangeEvent) string {
	if !event.Detail.Errored() {
		return ""
	}
	reason := strings.TrimSpace(event.Detail.StoppedReason)
	for _, c := range event.Detail.Containers {
		if c.Errored() {
			code := fmt.Sprintf("image build exited with code %d", c.ExitCode)
			if reason == "" {
				reason = code
			} else {
				reason = code + ": " + reason
			}
			break
		}
	}
	return reason
}

func (h *DeployTaskStateChangeHandler) UpdateApplicationsTable(ctx context.Context, applicationId string, finalState *FinalState, tableName string) error {
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
		TableName:                 aws.String(tableName),
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

package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/expression"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/pennsieve/app-deploy-service/status/events"
	"github.com/pennsieve/app-deploy-service/status/models"
)

// The sample document (docs/builds.md "The sample document"). curation-
// service's App Sample Ready event, once per app-sample job that ended, is
// recorded on the version as sample {status, documentKey, previewKey,
// crateKey, bytes, pages, public, error, ...} and published on the version's
// channel as application_sample_status_event. The version is found by the
// event's versionId; the table is the App Store versions table.

// SampleReadyEvent is the EventBridge envelope of a sample event.
type SampleReadyEvent struct {
	Id         string                   `json:"id"`
	DetailType string                   `json:"detail-type"`
	Source     string                   `json:"source"`
	Time       time.Time                `json:"time"`
	Detail     models.SampleReadyDetail `json:"detail"`
}

// HandleSampleReady records the sample document on its version.
func (h *DeployTaskStateChangeHandler) HandleSampleReady(ctx context.Context, event SampleReadyEvent) error {
	d := event.Detail
	logger := h.logger.With(slog.String("versionId", d.VersionId), slog.String("applicationId", d.ApplicationId), slog.String("tag", d.Tag), slog.String("jobId", d.JobId))
	if event.DetailType != models.SampleReadyDetailType {
		logger.Warn("ignoring event of another type", slog.String("detailType", event.DetailType))
		return nil
	}
	if strings.TrimSpace(d.VersionId) == "" {
		return fmt.Errorf("sample event %s names no version", event.Id)
	}
	if h.AppStoreVersionsTable == "" {
		return fmt.Errorf("no App Store versions table configured; cannot record the sample of version %s", d.VersionId)
	}
	rec := sampleFromEvent(d, h.nowString())
	in, err := SampleUpdate(d.VersionId, h.AppStoreVersionsTable, rec)
	if err != nil {
		return err
	}
	if _, err := h.DynamoDBApi.UpdateItem(ctx, in); err != nil {
		return fmt.Errorf("error recording the sample of version %s: %w", d.VersionId, err)
	}
	h.SendSampleStatusEvent(ctx, d.VersionId, rec)
	logger.Info("sample: recorded", slog.String("status", rec.Status), slog.String("documentKey", rec.DocumentKey))
	return nil
}

func (h *DeployTaskStateChangeHandler) nowString() string {
	if h.Fixtures != nil {
		return h.Fixtures.now()
	}
	return time.Now().UTC().Format(time.RFC3339)
}

// sampleFromEvent is the version's record of a sample event.
func sampleFromEvent(d models.SampleReadyDetail, now string) models.Sample {
	rec := models.Sample{JobId: d.JobId, Bucket: d.Bucket, UpdatedAt: now, GeneratedAt: d.At}
	if d.Status == models.SampleDocReady && d.Document != nil && d.Document.Key != "" {
		rec.Status = models.SampleDocReady
		rec.DocumentKey, rec.Sha256, rec.Bytes, rec.Pages = d.Document.Key, d.Document.Sha256, d.Document.Bytes, d.Document.Pages
		if rec.Pages == 0 {
			rec.Pages = d.Pages
		}
		rec.PreviewKey, rec.CrateKey, rec.Figures, rec.Narrative, rec.Public = d.PreviewKey, d.CrateKey, d.Figures, d.Narrative, d.Public
		return rec
	}
	rec.Status = models.SampleDocFailed
	rec.Error = d.Error
	if rec.Error == nil {
		rec.Error = &models.FixturesError{Code: "internal", Message: "the sample job ended without a document or an error"}
	}
	return rec
}

// SampleUpdate is the update HandleSampleReady makes.
func SampleUpdate(versionId, versionsTable string, rec models.Sample) (*dynamodb.UpdateItemInput, error) {
	expressions, err := expression.NewBuilder().
		WithCondition(expression.AttributeExists(expression.Name(models.ApplicationKeyField))).
		WithUpdate(expression.Set(expression.Name(models.SampleField), expression.Value(rec))).
		Build()
	if err != nil {
		return nil, fmt.Errorf("error building the sample update of %s: %w", versionId, err)
	}
	return &dynamodb.UpdateItemInput{
		Key:                       models.ApplicationKey(versionId),
		TableName:                 aws.String(versionsTable),
		ConditionExpression:       expressions.Condition(),
		ExpressionAttributeNames:  expressions.Names(),
		ExpressionAttributeValues: expressions.Values(),
		UpdateExpression:          expressions.Update(),
	}, nil
}

// SendSampleStatusEvent says how the version's sample document stands.
func (h *DeployTaskStateChangeHandler) SendSampleStatusEvent(ctx context.Context, versionId string, rec models.Sample) {
	event := events.SampleStatusEvent{
		ApplicationId: versionId,
		Status:        rec.Status,
		DocumentKey:   rec.DocumentKey,
		PreviewKey:    rec.PreviewKey,
		Pages:         rec.Pages,
		Source:        "DeployTaskStateChangeHandler",
	}
	if rec.Error != nil {
		event.ErrorCode, event.ErrorMessage = rec.Error.Code, rec.Error.Message
	}
	if t, err := time.Parse(time.RFC3339, rec.UpdatedAt); err == nil {
		event.Time = &t
	}
	h.publish(ctx, versionId, events.SampleStatusEventName, event)
}

// parseSampleReady reads a sample event from its raw JSON.
func parseSampleReady(raw json.RawMessage) (SampleReadyEvent, error) {
	var event SampleReadyEvent
	if err := json.Unmarshal(raw, &event); err != nil {
		return event, fmt.Errorf("error reading sample event: %w", err)
	}
	return event, nil
}

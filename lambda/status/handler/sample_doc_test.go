package handler

import (
	"context"
	"fmt"
	"testing"

	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/pennsieve/app-deploy-service/status/events"
	"github.com/pennsieve/app-deploy-service/status/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func sampleReadyEvent(status string, withDocument bool) []byte {
	doc := "null"
	if withDocument {
		doc = `{"key": "examples/apps/app-1/v1.0.0/abc.pdf", "sha256": "abc", "bytes": 120000, "pages": 6, "contentType": "application/pdf"}`
	}
	errField := ""
	if status == "failed" {
		errField = `, "error": {"code": "no_outputs", "message": "the app wrote no files in the sample run"}`
	}
	return []byte(fmt.Sprintf(`{
		"version": "0", "id": "s1", "detail-type": "App Sample Ready", "source": "pennsieve.curation-service",
		"time": "2026-11-11T22:00:00Z", "region": "us-east-1", "resources": ["version-1"],
		"detail": {"environment": "dev", "applicationId": "app-1", "versionId": "version-1", "tag": "v1.0.0",
			"jobId": "sample-abc", "runId": "pipeline:version-1:r1", "status": "%s", "bucket": "pennsieve-dev-curation-use1",
			"at": "2026-11-11T21:59:00.000Z", "document": %s, "previewKey": "examples/apps/app-1/v1.0.0/abc.preview.png",
			"crateKey": "examples/apps/app-1/v1.0.0/abc.ro-crate-metadata.json", "pages": 6, "figures": 3, "narrative": true,
			"public": {"document": "public/apps/app-1/v1.0.0/abc.pdf"}%s}
	}`, status, doc, errField))
}

func sampleRecorded(t *testing.T, db *recordingDynamoDB) *models.Sample {
	for _, u := range db.tableUpdates(testVersionsTable) {
		if v, ok := setValues(u)[models.SampleField]; ok {
			var rec models.Sample
			require.NoError(t, attributevalue.Unmarshal(v, &rec))
			return &rec
		}
	}
	return nil
}

func sampleStatusEvents(h *DeployTaskStateChangeHandler) []events.SampleStatusEvent {
	var out []events.SampleStatusEvent
	for _, p := range h.Realtime.(*fakeRealtime).published {
		if e, ok := p.event.(events.SampleStatusEvent); ok {
			out = append(out, e)
		}
	}
	return out
}

func TestHandleEvent_SampleReadyIsRecordedOnTheVersion(t *testing.T) {
	h, db, _, _, _ := newSampleTestHandler(sampleItems(4096, nil, readyFixtures()))
	h.WithAppStoreVersionsTable(testVersionsTable)
	require.NoError(t, h.HandleEvent(context.Background(), sampleReadyEvent("ready", true)))

	rec := sampleRecorded(t, db)
	require.NotNil(t, rec)
	assert.Equal(t, models.SampleDocReady, rec.Status)
	assert.Equal(t, "examples/apps/app-1/v1.0.0/abc.pdf", rec.DocumentKey)
	assert.Equal(t, "pennsieve-dev-curation-use1", rec.Bucket)
	assert.Equal(t, "abc", rec.Sha256)
	assert.Equal(t, int64(120000), rec.Bytes)
	assert.Equal(t, 6, rec.Pages)
	assert.Equal(t, 3, rec.Figures)
	assert.True(t, rec.Narrative)
	assert.Equal(t, "examples/apps/app-1/v1.0.0/abc.preview.png", rec.PreviewKey)
	assert.Equal(t, map[string]string{"document": "public/apps/app-1/v1.0.0/abc.pdf"}, rec.Public)
	assert.Equal(t, "sample-abc", rec.JobId)
	assert.Equal(t, "2026-11-11T21:59:00.000Z", rec.GeneratedAt)
	assert.Nil(t, rec.Error)

	live := sampleStatusEvents(h)
	require.Len(t, live, 1)
	assert.Equal(t, "version-1", live[0].ApplicationId)
	assert.Equal(t, models.SampleDocReady, live[0].Status)
	assert.Equal(t, rec.DocumentKey, live[0].DocumentKey)
	assert.Equal(t, 6, live[0].Pages)
}

func TestHandleEvent_SampleFailedCarriesTheError(t *testing.T) {
	h, db, _, _, _ := newSampleTestHandler(sampleItems(4096, nil, readyFixtures()))
	h.WithAppStoreVersionsTable(testVersionsTable)
	require.NoError(t, h.HandleEvent(context.Background(), sampleReadyEvent("failed", false)))
	rec := sampleRecorded(t, db)
	require.NotNil(t, rec)
	assert.Equal(t, models.SampleDocFailed, rec.Status)
	assert.Empty(t, rec.DocumentKey)
	require.NotNil(t, rec.Error)
	assert.Equal(t, "no_outputs", rec.Error.Code)
	live := sampleStatusEvents(h)
	require.Len(t, live, 1)
	assert.Equal(t, "no_outputs", live[0].ErrorCode)
}

func TestHandleEvent_SampleWithoutATableOrVersionIsAnError(t *testing.T) {
	h, db, _, _, _ := newSampleTestHandler(sampleItems(4096, nil, nil))
	require.Error(t, h.HandleEvent(context.Background(), sampleReadyEvent("ready", true)), "no versions table configured")
	assert.Nil(t, sampleRecorded(t, db))

	h.WithAppStoreVersionsTable(testVersionsTable)
	noVersion := []byte(`{"id": "s2", "detail-type": "App Sample Ready", "source": "pennsieve.curation-service", "detail": {"status": "ready"}}`)
	require.Error(t, h.HandleEvent(context.Background(), noVersion))
}

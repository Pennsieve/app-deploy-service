package store_dynamodb

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type ArgCaptureAppStoreVersionTableAPI struct {
	PutItemInput    *dynamodb.PutItemInput
	QueryInput      *dynamodb.QueryInput
	UpdateItemInput *dynamodb.UpdateItemInput

	QueryOutput *dynamodb.QueryOutput
}

func (m *ArgCaptureAppStoreVersionTableAPI) PutItem(_ context.Context, params *dynamodb.PutItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error) {
	m.PutItemInput = params
	return &dynamodb.PutItemOutput{}, nil
}

func (m *ArgCaptureAppStoreVersionTableAPI) Query(_ context.Context, params *dynamodb.QueryInput, _ ...func(*dynamodb.Options)) (*dynamodb.QueryOutput, error) {
	m.QueryInput = params
	if m.QueryOutput != nil {
		return m.QueryOutput, nil
	}
	return &dynamodb.QueryOutput{}, nil
}

func (m *ArgCaptureAppStoreVersionTableAPI) UpdateItem(_ context.Context, params *dynamodb.UpdateItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.UpdateItemOutput, error) {
	m.UpdateItemInput = params
	return &dynamodb.UpdateItemOutput{}, nil
}

func TestAppStoreVersionDatabaseStore_Insert(t *testing.T) {
	mock := &ArgCaptureAppStoreVersionTableAPI{}
	tableName := "test-versions-table"
	store := NewAppStoreVersionDatabaseStore(mock, tableName)

	version := AppStoreVersion{
		Uuid:           uuid.NewString(),
		ApplicationId:  uuid.NewString(),
		Version:        "v1.0.0",
		ReleaseId:      42,
		DestinationUrl: "123456789.dkr.ecr.us-east-1.amazonaws.com/my-app:v1.0.0",
		CreatedAt:      "2026-01-01",
		Status:         "deploying",
	}

	err := store.Insert(context.Background(), version)
	require.NoError(t, err)

	require.NotNil(t, mock.PutItemInput)
	assert.Equal(t, tableName, aws.ToString(mock.PutItemInput.TableName))

	var roundTripped AppStoreVersion
	err = attributevalue.UnmarshalMap(mock.PutItemInput.Item, &roundTripped)
	require.NoError(t, err)
	assert.Equal(t, version, roundTripped)
}

func TestAppStoreVersionDatabaseStore_GetByApplicationId(t *testing.T) {
	applicationId := uuid.NewString()
	v1 := AppStoreVersion{
		Uuid:          uuid.NewString(),
		ApplicationId: applicationId,
		Version:       "v1.0.0",
		Status:        "deployed",
		CreatedAt:     "2026-01-01",
	}
	v2 := AppStoreVersion{
		Uuid:          uuid.NewString(),
		ApplicationId: applicationId,
		Version:       "v2.0.0",
		Status:        "deploying",
		CreatedAt:     "2026-01-02",
	}

	item1, err := attributevalue.MarshalMap(v1)
	require.NoError(t, err)
	item2, err := attributevalue.MarshalMap(v2)
	require.NoError(t, err)

	tableName := "test-versions-table"
	mock := &ArgCaptureAppStoreVersionTableAPI{
		QueryOutput: &dynamodb.QueryOutput{
			Items: []map[string]types.AttributeValue{item1, item2},
			Count: 2,
		},
	}
	store := NewAppStoreVersionDatabaseStore(mock, tableName)

	versions, err := store.GetByApplicationId(context.Background(), applicationId)
	require.NoError(t, err)
	require.Len(t, versions, 2)
	assert.Equal(t, v1, versions[0])
	assert.Equal(t, v2, versions[1])

	// Verify query params
	require.NotNil(t, mock.QueryInput)
	assert.Equal(t, tableName, aws.ToString(mock.QueryInput.TableName))
	assert.Equal(t, "applicationId-version-index", aws.ToString(mock.QueryInput.IndexName))

	// Verify applicationId is in expression values
	var appIdValueKey string
	for k, v := range mock.QueryInput.ExpressionAttributeValues {
		if sv, ok := v.(*types.AttributeValueMemberS); ok && sv.Value == applicationId {
			appIdValueKey = k
		}
	}
	assert.NotEmpty(t, appIdValueKey, "applicationId value should be in ExpressionAttributeValues")
}

func TestAppStoreVersionDatabaseStore_GetByApplicationId_Empty(t *testing.T) {
	mock := &ArgCaptureAppStoreVersionTableAPI{
		QueryOutput: &dynamodb.QueryOutput{
			Items: []map[string]types.AttributeValue{},
			Count: 0,
		},
	}
	store := NewAppStoreVersionDatabaseStore(mock, "test-table")

	versions, err := store.GetByApplicationId(context.Background(), uuid.NewString())
	require.NoError(t, err)
	assert.Empty(t, versions)
}

func TestAppStoreVersionDatabaseStore_GetByApplicationIdAndVersion(t *testing.T) {
	applicationId := uuid.NewString()
	versionTag := "v1.0.0"
	v := AppStoreVersion{
		Uuid:           uuid.NewString(),
		ApplicationId:  applicationId,
		Version:        versionTag,
		ReleaseId:      7,
		DestinationUrl: "123456789.dkr.ecr.us-east-1.amazonaws.com/my-app:v1.0.0",
		Status:         "deployed",
		CreatedAt:      "2026-01-01",
	}
	item, err := attributevalue.MarshalMap(v)
	require.NoError(t, err)

	tableName := "test-versions-table"
	mock := &ArgCaptureAppStoreVersionTableAPI{
		QueryOutput: &dynamodb.QueryOutput{
			Items: []map[string]types.AttributeValue{item},
			Count: 1,
		},
	}
	store := NewAppStoreVersionDatabaseStore(mock, tableName)

	versions, err := store.GetByApplicationIdAndVersion(context.Background(), applicationId, versionTag)
	require.NoError(t, err)
	require.Len(t, versions, 1)
	assert.Equal(t, v, versions[0])

	// Verify query params
	require.NotNil(t, mock.QueryInput)
	assert.Equal(t, tableName, aws.ToString(mock.QueryInput.TableName))
	assert.Equal(t, "applicationId-version-index", aws.ToString(mock.QueryInput.IndexName))

	// Verify both applicationId and version are in expression values
	var hasAppId, hasVersion bool
	for _, val := range mock.QueryInput.ExpressionAttributeValues {
		if sv, ok := val.(*types.AttributeValueMemberS); ok {
			if sv.Value == applicationId {
				hasAppId = true
			}
			if sv.Value == versionTag {
				hasVersion = true
			}
		}
	}
	assert.True(t, hasAppId, "applicationId should be in ExpressionAttributeValues")
	assert.True(t, hasVersion, "version should be in ExpressionAttributeValues")
}

func TestAppStoreVersionDatabaseStore_GetByApplicationIdAndVersion_NotFound(t *testing.T) {
	mock := &ArgCaptureAppStoreVersionTableAPI{
		QueryOutput: &dynamodb.QueryOutput{
			Items: []map[string]types.AttributeValue{},
			Count: 0,
		},
	}
	store := NewAppStoreVersionDatabaseStore(mock, "test-table")

	versions, err := store.GetByApplicationIdAndVersion(context.Background(), uuid.NewString(), "v99.0.0")
	require.NoError(t, err)
	assert.Empty(t, versions)
}

func TestAppStoreVersionDatabaseStore_UpdateStatus(t *testing.T) {
	mock := &ArgCaptureAppStoreVersionTableAPI{}
	tableName := "test-versions-table"
	store := NewAppStoreVersionDatabaseStore(mock, tableName)

	versionUuid := uuid.NewString()
	newStatus := "deployed"

	err := store.UpdateStatus(context.Background(), newStatus, versionUuid)
	require.NoError(t, err)

	require.NotNil(t, mock.UpdateItemInput)
	assert.Equal(t, tableName, aws.ToString(mock.UpdateItemInput.TableName))

	// Verify the key contains the uuid
	assert.Equal(t, &types.AttributeValueMemberS{Value: versionUuid}, mock.UpdateItemInput.Key["uuid"])

	// Verify update expression sets registrationStatus
	assert.Equal(t, "set registrationStatus = :s", aws.ToString(mock.UpdateItemInput.UpdateExpression))
	assert.Equal(t, &types.AttributeValueMemberS{Value: newStatus}, mock.UpdateItemInput.ExpressionAttributeValues[":s"])
}

func TestAppStoreVersion_MarshalRoundTrip(t *testing.T) {
	original := AppStoreVersion{
		Uuid:           uuid.NewString(),
		ApplicationId:  uuid.NewString(),
		Version:        "v1.2.3",
		ReleaseId:      15,
		DestinationUrl: "123456789.dkr.ecr.us-east-1.amazonaws.com/my-app:v1.2.3",
		CreatedAt:      "2026-03-09 12:00:00 +0000 UTC",
		Status:         "deployed",
	}

	item, err := attributevalue.MarshalMap(original)
	require.NoError(t, err)

	var roundTripped AppStoreVersion
	err = attributevalue.UnmarshalMap(item, &roundTripped)
	require.NoError(t, err)

	assert.Equal(t, original, roundTripped)
}

func TestAppStoreVersion_StatusFieldName(t *testing.T) {
	// Verify that Status maps to "registrationStatus" in DynamoDB
	v := AppStoreVersion{
		Uuid:   "test-uuid",
		Status: "deployed",
	}
	item, err := attributevalue.MarshalMap(v)
	require.NoError(t, err)

	assert.Equal(t, &types.AttributeValueMemberS{Value: "deployed"}, item["registrationStatus"])
}

func TestAppStoreVersionDatabaseStore_ImplementsInterface(t *testing.T) {
	var _ AppStoreVersionDBStore = (*AppStoreVersionDatabaseStore)(nil)
}

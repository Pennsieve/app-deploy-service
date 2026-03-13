package store_dynamodb

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type ArgCaptureAppAccessTableAPI struct {
	PutItemInput    *dynamodb.PutItemInput
	QueryInput      *dynamodb.QueryInput
	DeleteItemInput *dynamodb.DeleteItemInput

	QueryOutput *dynamodb.QueryOutput
}

func (m *ArgCaptureAppAccessTableAPI) PutItem(_ context.Context, params *dynamodb.PutItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error) {
	m.PutItemInput = params
	return &dynamodb.PutItemOutput{}, nil
}

func (m *ArgCaptureAppAccessTableAPI) Query(_ context.Context, params *dynamodb.QueryInput, _ ...func(*dynamodb.Options)) (*dynamodb.QueryOutput, error) {
	m.QueryInput = params
	if m.QueryOutput != nil {
		return m.QueryOutput, nil
	}
	return &dynamodb.QueryOutput{}, nil
}

func (m *ArgCaptureAppAccessTableAPI) DeleteItem(_ context.Context, params *dynamodb.DeleteItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.DeleteItemOutput, error) {
	m.DeleteItemInput = params
	return &dynamodb.DeleteItemOutput{}, nil
}

func TestAppAccessDatabaseStore_Insert(t *testing.T) {
	mock := &ArgCaptureAppAccessTableAPI{}
	tableName := "test-app-access-table"
	store := NewAppAccessDatabaseStore(mock, tableName)

	access := AppAccess{
		EntityId:    "user#N:user:abc-123",
		AppId:       "app#some-uuid",
		EntityType:  "user",
		EntityRawId: "N:user:abc-123",
		AppUuid:     "some-uuid",
		AccessType:  "owner",
		GrantedAt:   "2026-01-01",
		GrantedBy:   "N:user:abc-123",
	}

	err := store.Insert(context.Background(), access)
	require.NoError(t, err)

	require.NotNil(t, mock.PutItemInput)
	assert.Equal(t, tableName, aws.ToString(mock.PutItemInput.TableName))

	var roundTripped AppAccess
	err = attributevalue.UnmarshalMap(mock.PutItemInput.Item, &roundTripped)
	require.NoError(t, err)
	assert.Equal(t, access, roundTripped)
}

func TestAppAccessDatabaseStore_GetByApp(t *testing.T) {
	access := AppAccess{
		EntityId:    "user#N:user:abc-123",
		AppId:       "app#some-uuid",
		EntityType:  "user",
		EntityRawId: "N:user:abc-123",
		AppUuid:     "some-uuid",
		AccessType:  "owner",
		GrantedAt:   "2026-01-01",
		GrantedBy:   "N:user:abc-123",
	}
	item, err := attributevalue.MarshalMap(access)
	require.NoError(t, err)

	mock := &ArgCaptureAppAccessTableAPI{
		QueryOutput: &dynamodb.QueryOutput{
			Items: []map[string]types.AttributeValue{item},
			Count: 1,
		},
	}
	store := NewAppAccessDatabaseStore(mock, "test-table")

	items, err := store.GetByApp(context.Background(), "some-uuid")
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, access, items[0])

	require.NotNil(t, mock.QueryInput)
	assert.Equal(t, "appId-entityId-index", aws.ToString(mock.QueryInput.IndexName))
}

func TestAppAccessDatabaseStore_GetByApp_Empty(t *testing.T) {
	mock := &ArgCaptureAppAccessTableAPI{
		QueryOutput: &dynamodb.QueryOutput{
			Items: []map[string]types.AttributeValue{},
			Count: 0,
		},
	}
	store := NewAppAccessDatabaseStore(mock, "test-table")

	items, err := store.GetByApp(context.Background(), "nonexistent-uuid")
	require.NoError(t, err)
	assert.Empty(t, items)
}

func TestAppAccessDatabaseStore_GetByEntity(t *testing.T) {
	access := AppAccess{
		EntityId:    "user#N:user:abc-123",
		AppId:       "app#some-uuid",
		EntityType:  "user",
		EntityRawId: "N:user:abc-123",
		AppUuid:     "some-uuid",
		AccessType:  "shared",
		GrantedAt:   "2026-01-01",
		GrantedBy:   "N:user:owner-id",
	}
	item, err := attributevalue.MarshalMap(access)
	require.NoError(t, err)

	mock := &ArgCaptureAppAccessTableAPI{
		QueryOutput: &dynamodb.QueryOutput{
			Items: []map[string]types.AttributeValue{item},
			Count: 1,
		},
	}
	store := NewAppAccessDatabaseStore(mock, "test-table")

	items, err := store.GetByEntity(context.Background(), "user#N:user:abc-123")
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, access, items[0])

	require.NotNil(t, mock.QueryInput)
	assert.Nil(t, mock.QueryInput.IndexName)
}

func TestAppAccessDatabaseStore_GetAccess(t *testing.T) {
	access := AppAccess{
		EntityId:    "user#N:user:abc-123",
		AppId:       "app#some-uuid",
		EntityType:  "user",
		EntityRawId: "N:user:abc-123",
		AppUuid:     "some-uuid",
		AccessType:  "owner",
		GrantedAt:   "2026-01-01",
		GrantedBy:   "N:user:abc-123",
	}
	item, err := attributevalue.MarshalMap(access)
	require.NoError(t, err)

	mock := &ArgCaptureAppAccessTableAPI{
		QueryOutput: &dynamodb.QueryOutput{
			Items: []map[string]types.AttributeValue{item},
			Count: 1,
		},
	}
	store := NewAppAccessDatabaseStore(mock, "test-table")

	result, err := store.GetAccess(context.Background(), "user#N:user:abc-123", "app#some-uuid")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, access, *result)
}

func TestAppAccessDatabaseStore_GetAccess_NotFound(t *testing.T) {
	mock := &ArgCaptureAppAccessTableAPI{
		QueryOutput: &dynamodb.QueryOutput{
			Items: []map[string]types.AttributeValue{},
			Count: 0,
		},
	}
	store := NewAppAccessDatabaseStore(mock, "test-table")

	result, err := store.GetAccess(context.Background(), "user#nonexistent", "app#nonexistent")
	require.NoError(t, err)
	assert.Nil(t, result)
}

func TestAppAccessDatabaseStore_Delete(t *testing.T) {
	mock := &ArgCaptureAppAccessTableAPI{}
	tableName := "test-table"
	store := NewAppAccessDatabaseStore(mock, tableName)

	err := store.Delete(context.Background(), "user#N:user:abc-123", "app#some-uuid")
	require.NoError(t, err)

	require.NotNil(t, mock.DeleteItemInput)
	assert.Equal(t, tableName, aws.ToString(mock.DeleteItemInput.TableName))
	assert.Contains(t, mock.DeleteItemInput.Key, "entityId")
	assert.Contains(t, mock.DeleteItemInput.Key, "appId")
}

func TestAppAccessDatabaseStore_ImplementsInterface(t *testing.T) {
	var _ AppAccessDBStore = (*AppAccessDatabaseStore)(nil)
}

func TestAppAccess_MarshalRoundTrip(t *testing.T) {
	original := AppAccess{
		EntityId:       "user#N:user:abc-123",
		AppId:          "app#some-uuid",
		EntityType:     "user",
		EntityRawId:    "N:user:abc-123",
		AppUuid:        "some-uuid",
		AccessType:     "owner",
		OrganizationId: "N:org:xyz-789",
		GrantedAt:      "2026-01-01 00:00:00 +0000 UTC",
		GrantedBy:      "N:user:abc-123",
	}

	item, err := attributevalue.MarshalMap(original)
	require.NoError(t, err)

	var roundTripped AppAccess
	err = attributevalue.UnmarshalMap(item, &roundTripped)
	require.NoError(t, err)
	assert.Equal(t, original, roundTripped)
}

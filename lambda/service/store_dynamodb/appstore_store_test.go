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

type ArgCaptureAppStoreTableAPI struct {
	PutItemInput    *dynamodb.PutItemInput
	QueryInput      *dynamodb.QueryInput
	ScanInput       *dynamodb.ScanInput
	GetItemInput    *dynamodb.GetItemInput
	UpdateItemInput *dynamodb.UpdateItemInput

	QueryOutput   *dynamodb.QueryOutput
	ScanOutput    *dynamodb.ScanOutput
	GetItemOutput *dynamodb.GetItemOutput
}

func (m *ArgCaptureAppStoreTableAPI) PutItem(_ context.Context, params *dynamodb.PutItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error) {
	m.PutItemInput = params
	return &dynamodb.PutItemOutput{}, nil
}

func (m *ArgCaptureAppStoreTableAPI) Query(_ context.Context, params *dynamodb.QueryInput, _ ...func(*dynamodb.Options)) (*dynamodb.QueryOutput, error) {
	m.QueryInput = params
	if m.QueryOutput != nil {
		return m.QueryOutput, nil
	}
	return &dynamodb.QueryOutput{}, nil
}

func (m *ArgCaptureAppStoreTableAPI) Scan(_ context.Context, params *dynamodb.ScanInput, _ ...func(*dynamodb.Options)) (*dynamodb.ScanOutput, error) {
	m.ScanInput = params
	if m.ScanOutput != nil {
		return m.ScanOutput, nil
	}
	return &dynamodb.ScanOutput{}, nil
}

func (m *ArgCaptureAppStoreTableAPI) GetItem(_ context.Context, params *dynamodb.GetItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error) {
	m.GetItemInput = params
	if m.GetItemOutput != nil {
		return m.GetItemOutput, nil
	}
	return &dynamodb.GetItemOutput{}, nil
}

func (m *ArgCaptureAppStoreTableAPI) UpdateItem(_ context.Context, params *dynamodb.UpdateItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.UpdateItemOutput, error) {
	m.UpdateItemInput = params
	return &dynamodb.UpdateItemOutput{}, nil
}

func TestAppStoreDatabaseStore_Insert(t *testing.T) {
	mock := &ArgCaptureAppStoreTableAPI{}
	tableName := "test-appstore-table"
	store := NewAppStoreDatabaseStore(mock, tableName)

	app := AppStoreApplication{
		Uuid:       uuid.NewString(),
		SourceUrl:  "https://github.com/test/repo",
		SourceType: "github",
		IsPrivate:  true,
		CreatedAt:  "2026-01-01",
	}

	err := store.Insert(context.Background(), app)
	require.NoError(t, err)

	require.NotNil(t, mock.PutItemInput)
	assert.Equal(t, tableName, aws.ToString(mock.PutItemInput.TableName))

	// Verify the item was marshaled correctly
	var roundTripped AppStoreApplication
	err = attributevalue.UnmarshalMap(mock.PutItemInput.Item, &roundTripped)
	require.NoError(t, err)
	assert.Equal(t, app, roundTripped)
}

func TestAppStoreDatabaseStore_GetBySourceUrl(t *testing.T) {
	sourceUrl := "https://github.com/test/repo"
	app := AppStoreApplication{
		Uuid:       uuid.NewString(),
		SourceUrl:  sourceUrl,
		SourceType: "github",
		IsPrivate:  false,
		CreatedAt:  "2026-01-01",
	}
	item, err := attributevalue.MarshalMap(app)
	require.NoError(t, err)

	tableName := "test-appstore-table"
	mock := &ArgCaptureAppStoreTableAPI{
		QueryOutput: &dynamodb.QueryOutput{
			Items: []map[string]types.AttributeValue{item},
			Count: 1,
		},
	}
	store := NewAppStoreDatabaseStore(mock, tableName)

	apps, err := store.GetBySourceUrl(context.Background(), sourceUrl)
	require.NoError(t, err)
	require.Len(t, apps, 1)
	assert.Equal(t, app, apps[0])

	// Verify query params
	require.NotNil(t, mock.QueryInput)
	assert.Equal(t, tableName, aws.ToString(mock.QueryInput.TableName))
	assert.Equal(t, "sourceUrl-index", aws.ToString(mock.QueryInput.IndexName))

	// Verify the expression contains sourceUrl value
	var sourceUrlValueKey string
	for k, v := range mock.QueryInput.ExpressionAttributeValues {
		if sv, ok := v.(*types.AttributeValueMemberS); ok && sv.Value == sourceUrl {
			sourceUrlValueKey = k
		}
	}
	assert.NotEmpty(t, sourceUrlValueKey, "sourceUrl value should be in ExpressionAttributeValues")
}

func TestAppStoreDatabaseStore_GetBySourceUrl_Empty(t *testing.T) {
	mock := &ArgCaptureAppStoreTableAPI{
		QueryOutput: &dynamodb.QueryOutput{
			Items: []map[string]types.AttributeValue{},
			Count: 0,
		},
	}
	store := NewAppStoreDatabaseStore(mock, "test-table")

	apps, err := store.GetBySourceUrl(context.Background(), "https://github.com/nonexistent/repo")
	require.NoError(t, err)
	assert.Empty(t, apps)
}

func TestAppStoreDatabaseStore_GetAll(t *testing.T) {
	app1 := AppStoreApplication{Uuid: uuid.NewString(), SourceUrl: "https://github.com/org/repo1", SourceType: "github", IsPrivate: false, CreatedAt: "2026-01-01"}
	app2 := AppStoreApplication{Uuid: uuid.NewString(), SourceUrl: "https://github.com/org/repo2", SourceType: "github", IsPrivate: true, CreatedAt: "2026-01-02"}

	item1, err := attributevalue.MarshalMap(app1)
	require.NoError(t, err)
	item2, err := attributevalue.MarshalMap(app2)
	require.NoError(t, err)

	tableName := "test-appstore-table"
	mock := &ArgCaptureAppStoreTableAPI{
		ScanOutput: &dynamodb.ScanOutput{
			Items: []map[string]types.AttributeValue{item1, item2},
			Count: 2,
		},
	}
	store := NewAppStoreDatabaseStore(mock, tableName)

	apps, err := store.GetAll(context.Background())
	require.NoError(t, err)
	require.Len(t, apps, 2)
	assert.Equal(t, app1, apps[0])
	assert.Equal(t, app2, apps[1])

	require.NotNil(t, mock.ScanInput)
	assert.Equal(t, tableName, aws.ToString(mock.ScanInput.TableName))
}

func TestAppStoreDatabaseStore_GetAll_Empty(t *testing.T) {
	mock := &ArgCaptureAppStoreTableAPI{
		ScanOutput: &dynamodb.ScanOutput{
			Items: []map[string]types.AttributeValue{},
			Count: 0,
		},
	}
	store := NewAppStoreDatabaseStore(mock, "test-table")

	apps, err := store.GetAll(context.Background())
	require.NoError(t, err)
	assert.Empty(t, apps)
}

func TestAppStoreApplication_MarshalRoundTrip(t *testing.T) {
	original := AppStoreApplication{
		Uuid:       uuid.NewString(),
		SourceUrl:  "https://github.com/pennsieve/test-app",
		SourceType: "github",
		IsPrivate:  true,
		CreatedAt:  "2026-03-09 12:00:00 +0000 UTC",
	}

	item, err := attributevalue.MarshalMap(original)
	require.NoError(t, err)

	var roundTripped AppStoreApplication
	err = attributevalue.UnmarshalMap(item, &roundTripped)
	require.NoError(t, err)

	assert.Equal(t, original, roundTripped)
}

func TestAppStoreApplication_GetKey(t *testing.T) {
	app := AppStoreApplication{Uuid: "test-uuid-123"}
	key := app.GetKey()
	assert.Equal(t, &types.AttributeValueMemberS{Value: "test-uuid-123"}, key["uuid"])
	assert.Len(t, key, 1)
}

func TestAppStoreApplication_IsPrivateDefaultsFalse(t *testing.T) {
	item := map[string]types.AttributeValue{
		"uuid":       &types.AttributeValueMemberS{Value: "test-uuid"},
		"sourceUrl":  &types.AttributeValueMemberS{Value: "https://github.com/test/repo"},
		"sourceType": &types.AttributeValueMemberS{Value: "github"},
		"createdAt":  &types.AttributeValueMemberS{Value: "2026-01-01"},
	}

	var app AppStoreApplication
	err := attributevalue.UnmarshalMap(item, &app)
	require.NoError(t, err)
	assert.False(t, app.IsPrivate)
	assert.Equal(t, "test-uuid", app.Uuid)
}

func TestAppStoreDatabaseStore_GetById(t *testing.T) {
	app := AppStoreApplication{
		Uuid:       "test-uuid-123",
		SourceUrl:  "https://github.com/test/repo",
		SourceType: "github",
		IsPrivate:  false,
		Visibility: "public",
		OwnerId:    "N:user:abc-123",
		CreatedAt:  "2026-01-01",
	}
	item, err := attributevalue.MarshalMap(app)
	require.NoError(t, err)

	mock := &ArgCaptureAppStoreTableAPI{
		GetItemOutput: &dynamodb.GetItemOutput{
			Item: item,
		},
	}
	store := NewAppStoreDatabaseStore(mock, "test-table")

	result, err := store.GetById(context.Background(), "test-uuid-123")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, app, *result)
}

func TestAppStoreDatabaseStore_GetById_NotFound(t *testing.T) {
	mock := &ArgCaptureAppStoreTableAPI{
		GetItemOutput: &dynamodb.GetItemOutput{
			Item: nil,
		},
	}
	store := NewAppStoreDatabaseStore(mock, "test-table")

	result, err := store.GetById(context.Background(), "nonexistent")
	require.NoError(t, err)
	assert.Nil(t, result)
}

func TestAppStoreDatabaseStore_UpdateVisibility(t *testing.T) {
	mock := &ArgCaptureAppStoreTableAPI{}
	tableName := "test-table"
	store := NewAppStoreDatabaseStore(mock, tableName)

	err := store.UpdateVisibility(context.Background(), "test-uuid", "private")
	require.NoError(t, err)
	require.NotNil(t, mock.UpdateItemInput)
	assert.Equal(t, tableName, aws.ToString(mock.UpdateItemInput.TableName))
}

func TestAppStoreApplication_VisibilityAndOwnerFields(t *testing.T) {
	original := AppStoreApplication{
		Uuid:       "test-uuid",
		SourceUrl:  "https://github.com/test/repo",
		SourceType: "github",
		IsPrivate:  true,
		Visibility: "private",
		OwnerId:    "N:user:owner-123",
		CreatedAt:  "2026-01-01",
	}

	item, err := attributevalue.MarshalMap(original)
	require.NoError(t, err)

	var roundTripped AppStoreApplication
	err = attributevalue.UnmarshalMap(item, &roundTripped)
	require.NoError(t, err)
	assert.Equal(t, original, roundTripped)
	assert.Equal(t, "private", roundTripped.Visibility)
	assert.Equal(t, "N:user:owner-123", roundTripped.OwnerId)
}

func TestAppStoreDatabaseStore_ImplementsInterface(t *testing.T) {
	var _ AppStoreDBStore = (*AppStoreDatabaseStore)(nil)
}

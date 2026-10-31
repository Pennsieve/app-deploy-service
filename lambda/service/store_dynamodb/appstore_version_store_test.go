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
	GetItemInput    *dynamodb.GetItemInput

	QueryOutput   *dynamodb.QueryOutput
	GetItemOutput *dynamodb.GetItemOutput
}

func (m *ArgCaptureAppStoreVersionTableAPI) GetItem(_ context.Context, params *dynamodb.GetItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error) {
	m.GetItemInput = params
	if m.GetItemOutput != nil {
		return m.GetItemOutput, nil
	}
	return &dynamodb.GetItemOutput{}, nil
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

func TestAppStoreVersionDatabaseStore_GetById(t *testing.T) {
	version := AppStoreVersion{Uuid: "version-1", ApplicationId: "app-1", Version: "1.0.0"}
	item, err := attributevalue.MarshalMap(version)
	require.NoError(t, err)
	mock := &ArgCaptureAppStoreVersionTableAPI{GetItemOutput: &dynamodb.GetItemOutput{Item: item}}
	store := NewAppStoreVersionDatabaseStore(mock, "versions-table")

	got, err := store.GetById(context.Background(), "version-1")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "app-1", got.ApplicationId)
	assert.Equal(t, "versions-table", aws.ToString(mock.GetItemInput.TableName))
	assert.Equal(t, &types.AttributeValueMemberS{Value: "version-1"}, mock.GetItemInput.Key["uuid"])
}

func TestAppStoreVersionDatabaseStore_GetById_NotFound(t *testing.T) {
	store := NewAppStoreVersionDatabaseStore(&ArgCaptureAppStoreVersionTableAPI{}, "versions-table")
	got, err := store.GetById(context.Background(), "missing")
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestAppStoreVersionDatabaseStore_SetManifest(t *testing.T) {
	mock := &ArgCaptureAppStoreVersionTableAPI{}
	store := NewAppStoreVersionDatabaseStore(mock, "test-versions-table")
	versionUuid := uuid.NewString()

	manifest := &AppManifest{
		Name:    "spike-detect",
		Runtime: AppRuntime{CPU: 2048, ComputeTypes: []string{"standard"}},
		Inputs:  []AppPort{{Name: "recording", MediaTypes: []string{"application/edf"}}},
	}
	require.NoError(t, store.SetManifest(context.Background(), versionUuid, manifest))

	require.NotNil(t, mock.UpdateItemInput)
	assert.Equal(t, &types.AttributeValueMemberS{Value: versionUuid}, mock.UpdateItemInput.Key["uuid"])
	assert.Equal(t, "set manifest = :m", aws.ToString(mock.UpdateItemInput.UpdateExpression))

	var got AppManifest
	require.NoError(t, attributevalue.Unmarshal(mock.UpdateItemInput.ExpressionAttributeValues[":m"], &got))
	assert.Equal(t, *manifest, got)
}

func TestAppStoreVersion_ManifestRoundTrip(t *testing.T) {
	v := AppStoreVersion{Uuid: "u", Version: "v1", Manifest: &AppManifest{Name: "x", Outputs: []AppPort{{Name: "out"}}}}
	item, err := attributevalue.MarshalMap(v)
	require.NoError(t, err)
	var back AppStoreVersion
	require.NoError(t, attributevalue.UnmarshalMap(item, &back))
	assert.Equal(t, v, back)

	noManifest, err := attributevalue.MarshalMap(AppStoreVersion{Uuid: "u"})
	require.NoError(t, err)
	_, has := noManifest["manifest"]
	assert.False(t, has, "a version without app.yml stores no manifest")
}

func TestAppStoreVersionDatabaseStore_SetRegistrationDetails(t *testing.T) {
	api := &ArgCaptureAppStoreVersionTableAPI{}
	store := NewAppStoreVersionDatabaseStore(api, "versions")

	assert.NoError(t, store.SetRegistrationDetails(context.Background(), "v1", "MIT", []string{"/runtime: x"}))
	assert.Equal(t, "set license = :l, manifestProblems = :p", aws.ToString(api.UpdateItemInput.UpdateExpression))
	assert.Equal(t, &types.AttributeValueMemberS{Value: "MIT"}, api.UpdateItemInput.ExpressionAttributeValues[":l"])

	assert.NoError(t, store.SetRegistrationDetails(context.Background(), "v1", "", nil))
	assert.Equal(t, "remove license, manifestProblems", aws.ToString(api.UpdateItemInput.UpdateExpression))
	assert.Nil(t, api.UpdateItemInput.ExpressionAttributeValues)
}

func TestAppStoreVersionDatabaseStore_SetCitation(t *testing.T) {
	api := &ArgCaptureAppStoreVersionTableAPI{}
	store := NewAppStoreVersionDatabaseStore(api, "versions")

	citation := &AppCitation{
		Title:   "FlowSOM clustering",
		Authors: []CitationAuthor{{Name: "Sofie Van Gassen", GivenNames: "Sofie", FamilyNames: "Van Gassen", Orcid: "0000-0002-7119-5330"}},
		RelatedWorks: []RelatedWork{
			{DOI: "10.1002/cyto.a.22625", RelationType: "References", Title: "FlowSOM", Year: 2015, Source: "references"},
			{RelationType: "References", Title: "FlowKit", URL: "https://github.com/whitews/FlowKit", Source: "references"},
		},
	}
	require.NoError(t, store.SetCitation(context.Background(), "v1", citation))
	assert.Equal(t, "set citation = :c", aws.ToString(api.UpdateItemInput.UpdateExpression))
	var got AppCitation
	require.NoError(t, attributevalue.Unmarshal(api.UpdateItemInput.ExpressionAttributeValues[":c"], &got))
	assert.Equal(t, *citation, got)

	require.NoError(t, store.SetCitation(context.Background(), "v1", nil))
	assert.Equal(t, "remove citation", aws.ToString(api.UpdateItemInput.UpdateExpression))
	assert.Nil(t, api.UpdateItemInput.ExpressionAttributeValues)
}

func TestAppStoreVersionDatabaseStore_SetReview(t *testing.T) {
	api := &ArgCaptureAppStoreVersionTableAPI{}
	store := NewAppStoreVersionDatabaseStore(api, "versions")

	review := &AppReview{
		Version: 1,
		Generate: &ReviewGenerate{Command: "python -m tests.make_fixture \"$REVIEW_INPUT_DIR\"", Root: "linked",
			Source: &ReviewDataset{DOI: "10.5281/zenodo.15388817", Files: []string{"*.fcs"}}},
		Inputs:     map[string]ReviewInput{"fcs": {Files: []string{"tests/fixtures/*.fcs"}}},
		Parameters: map[string]string{"N": "2000"},
	}
	require.NoError(t, store.SetReview(context.Background(), "v1", review))
	assert.Equal(t, "set review = :r", aws.ToString(api.UpdateItemInput.UpdateExpression))
	var got AppReview
	require.NoError(t, attributevalue.Unmarshal(api.UpdateItemInput.ExpressionAttributeValues[":r"], &got))
	assert.Equal(t, *review, got)

	require.NoError(t, store.SetReview(context.Background(), "v1", nil))
	assert.Equal(t, "remove review", aws.ToString(api.UpdateItemInput.UpdateExpression))
	assert.Nil(t, api.UpdateItemInput.ExpressionAttributeValues)
}

func dynasdVariants() VersionVariants {
	cpu := &AppManifest{Name: "DynaSD", Runtime: AppRuntime{CPU: 4096, Memory: 8192, ComputeTypes: []string{"standard"}},
		Parameters: []AppParameter{{Name: "MODEL", DefaultValue: "lindda", ValidValues: []string{"lindda", "oncet"}}},
		Variant:    &AppManifestVariant{ID: "cpu", Name: "CPU"}}
	gpu := &AppManifest{Name: "DynaSD", Runtime: AppRuntime{CPU: 4096, Memory: 14336, ComputeTypes: []string{"gpu"}},
		Parameters: []AppParameter{{Name: "MODEL", DefaultValue: "oncet", ValidValues: []string{"lindda", "oncet"}}},
		Variant:    &AppManifestVariant{ID: "gpu", Name: "GPU (ONCET)", Description: "CUDA PyTorch."}}
	return VersionVariants{
		DefaultVariant: "cpu",
		VariantOrder:   []string{"cpu", "gpu"},
		Variants: map[string]AppVariant{
			"cpu": {Name: "CPU", Manifest: cpu, Build: &AppBuild{Args: map[string]string{"TORCH_FLAVOUR": "cpu"}}},
			"gpu": {Name: "GPU (ONCET)", Description: "CUDA PyTorch.", Manifest: gpu,
				Build: &AppBuild{Args: map[string]string{"TORCH_FLAVOUR": "cuda"}, Target: "gpu", Dockerfile: "docker/Dockerfile"}},
		},
	}
}

func TestAppStoreVersionDatabaseStore_SetVariants(t *testing.T) {
	api := &ArgCaptureAppStoreVersionTableAPI{}
	store := NewAppStoreVersionDatabaseStore(api, "versions")
	vv := dynasdVariants()

	require.NoError(t, store.SetVariants(context.Background(), "v1", vv))
	assert.Equal(t, &types.AttributeValueMemberS{Value: "v1"}, api.UpdateItemInput.Key["uuid"])
	assert.Equal(t, "set defaultVariant = :d, variantOrder = :o, variants = :v remove build", aws.ToString(api.UpdateItemInput.UpdateExpression))
	assert.Equal(t, &types.AttributeValueMemberS{Value: "cpu"}, api.UpdateItemInput.ExpressionAttributeValues[":d"])
	var order []string
	require.NoError(t, attributevalue.Unmarshal(api.UpdateItemInput.ExpressionAttributeValues[":o"], &order))
	assert.Equal(t, vv.VariantOrder, order)
	// a DynamoDB map keyed by id, so variants.<id> can be updated on its own
	m, ok := api.UpdateItemInput.ExpressionAttributeValues[":v"].(*types.AttributeValueMemberM)
	require.True(t, ok, "variants is stored as a map")
	assert.Contains(t, m.Value, "gpu")
	var back map[string]AppVariant
	require.NoError(t, attributevalue.Unmarshal(m, &back))
	assert.Equal(t, vv.Variants, back)

	// a version without variants but with a top-level build
	require.NoError(t, store.SetVariants(context.Background(), "v2", VersionVariants{Build: &AppBuild{Target: "t"}}))
	assert.Equal(t, "set build = :b remove defaultVariant, variantOrder, variants", aws.ToString(api.UpdateItemInput.UpdateExpression))
}

// A version with variants survives a write and a read; one without has no
// variant attributes at all, so its record is as it always was.
func TestAppStoreVersion_VariantsRoundTrip(t *testing.T) {
	vv := dynasdVariants()
	v := AppStoreVersion{Uuid: "u", ApplicationId: "a", Version: "v0.3.0", Status: "registering",
		Manifest: vv.Variants["cpu"].Manifest, DefaultVariant: vv.DefaultVariant, VariantOrder: vv.VariantOrder, Variants: vv.Variants}
	item, err := attributevalue.MarshalMap(v)
	require.NoError(t, err)
	mock := &ArgCaptureAppStoreVersionTableAPI{GetItemOutput: &dynamodb.GetItemOutput{Item: item}}
	got, err := NewAppStoreVersionDatabaseStore(mock, "versions").GetById(context.Background(), "u")
	require.NoError(t, err)
	assert.Equal(t, v, *got)
	assert.Equal(t, "oncet", got.Variants["gpu"].Manifest.Parameters[0].DefaultValue)

	plain := AppStoreVersion{Uuid: "u", Version: "v0.2.2", Manifest: &AppManifest{Name: "x"}}
	item, err = attributevalue.MarshalMap(plain)
	require.NoError(t, err)
	for _, attr := range []string{"defaultVariant", "variantOrder", "variants", "build"} {
		_, has := item[attr]
		assert.False(t, has, "a version without variants has no %s", attr)
	}
	_, has := item["manifest"].(*types.AttributeValueMemberM).Value["variant"]
	assert.False(t, has, "nor a manifest variant")
}

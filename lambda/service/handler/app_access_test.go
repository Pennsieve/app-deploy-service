package handler

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/pennsieve/app-deploy-service/service/store_dynamodb"
	"github.com/pennsieve/pennsieve-go-core/pkg/authorizer"
	"github.com/pennsieve/pennsieve-go-core/pkg/models/organization"
	"github.com/pennsieve/pennsieve-go-core/pkg/models/teamUser"
	"github.com/pennsieve/pennsieve-go-core/pkg/models/user"
	"github.com/stretchr/testify/assert"
)

type mockAppAccessTableAPI struct {
	QueryOutputs map[string]*dynamodb.QueryOutput
}

func (m *mockAppAccessTableAPI) PutItem(_ context.Context, _ *dynamodb.PutItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error) {
	return &dynamodb.PutItemOutput{}, nil
}

func (m *mockAppAccessTableAPI) Query(_ context.Context, params *dynamodb.QueryInput, _ ...func(*dynamodb.Options)) (*dynamodb.QueryOutput, error) {
	for k, v := range params.ExpressionAttributeValues {
		if sv, ok := v.(*types.AttributeValueMemberS); ok {
			if output, found := m.QueryOutputs[k+":"+sv.Value]; found {
				return output, nil
			}
		}
	}
	return &dynamodb.QueryOutput{Items: []map[string]types.AttributeValue{}}, nil
}

func (m *mockAppAccessTableAPI) DeleteItem(_ context.Context, _ *dynamodb.DeleteItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.DeleteItemOutput, error) {
	return &dynamodb.DeleteItemOutput{}, nil
}

func (m *mockAppAccessTableAPI) BatchWriteItem(_ context.Context, _ *dynamodb.BatchWriteItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.BatchWriteItemOutput, error) {
	return &dynamodb.BatchWriteItemOutput{}, nil
}

func newTestClaims(userId string, orgId string, teams []teamUser.Claim) *authorizer.Claims {
	return &authorizer.Claims{
		UserClaim: &user.Claim{
			NodeId: userId,
		},
		OrgClaim: &organization.Claim{
			NodeId: orgId,
		},
		TeamClaims: teams,
	}
}

func TestCanAccessApp_PublicApp(t *testing.T) {
	app := &store_dynamodb.AppStoreApplication{
		Uuid:       "app-uuid",
		Visibility: "public",
		OwnerId:    "N:user:other",
	}
	claims := newTestClaims("N:user:viewer", "N:org:org1", nil)
	mock := &mockAppAccessTableAPI{QueryOutputs: map[string]*dynamodb.QueryOutput{}}
	store := store_dynamodb.NewAppAccessDatabaseStore(mock, "test-table")

	assert.True(t, CanAccessApp(context.Background(), claims, app, store))
}

func TestCanAccessApp_Owner(t *testing.T) {
	app := &store_dynamodb.AppStoreApplication{
		Uuid:       "app-uuid",
		Visibility: "private",
		OwnerId:    "N:user:owner-123",
	}
	claims := newTestClaims("N:user:owner-123", "N:org:org1", nil)
	mock := &mockAppAccessTableAPI{QueryOutputs: map[string]*dynamodb.QueryOutput{}}
	store := store_dynamodb.NewAppAccessDatabaseStore(mock, "test-table")

	assert.True(t, CanAccessApp(context.Background(), claims, app, store))
}

func TestCanAccessApp_PrivateNoAccess(t *testing.T) {
	app := &store_dynamodb.AppStoreApplication{
		Uuid:       "app-uuid",
		Visibility: "private",
		OwnerId:    "N:user:other",
	}
	claims := newTestClaims("N:user:viewer", "N:org:org1", nil)
	mock := &mockAppAccessTableAPI{QueryOutputs: map[string]*dynamodb.QueryOutput{}}
	store := store_dynamodb.NewAppAccessDatabaseStore(mock, "test-table")

	assert.False(t, CanAccessApp(context.Background(), claims, app, store))
}

func TestCanAccessApp_SharedUserAccess(t *testing.T) {
	app := &store_dynamodb.AppStoreApplication{
		Uuid:       "app-uuid",
		Visibility: "private",
		OwnerId:    "N:user:other",
	}

	access := store_dynamodb.AppAccess{
		EntityId:    "user#N:user:shared-user",
		AppId:       "app#app-uuid",
		EntityType:  "user",
		EntityRawId: "N:user:shared-user",
		AppUuid:     "app-uuid",
		AccessType:  "shared",
	}
	item, _ := attributevalue.MarshalMap(access)

	mock := &mockAppAccessTableAPI{
		QueryOutputs: map[string]*dynamodb.QueryOutput{},
	}
	for k := range map[string]bool{":0": true, ":1": true} {
		mock.QueryOutputs[k+":user#N:user:shared-user"] = &dynamodb.QueryOutput{
			Items: []map[string]types.AttributeValue{item},
			Count: 1,
		}
	}

	claims := newTestClaims("N:user:shared-user", "N:org:org1", nil)
	store := store_dynamodb.NewAppAccessDatabaseStore(mock, "test-table")

	result := CanAccessApp(context.Background(), claims, app, store)
	assert.True(t, result)
}

func TestCanAccessApp_WorkspaceAccess(t *testing.T) {
	app := &store_dynamodb.AppStoreApplication{
		Uuid:       "app-uuid",
		Visibility: "private",
		OwnerId:    "N:user:other",
	}

	access := store_dynamodb.AppAccess{
		EntityId:   "workspace#N:org:org1",
		AppId:      "app#app-uuid",
		EntityType: "workspace",
		AppUuid:    "app-uuid",
		AccessType: "workspace",
	}
	item, _ := attributevalue.MarshalMap(access)

	mock := &mockAppAccessTableAPI{
		QueryOutputs: map[string]*dynamodb.QueryOutput{},
	}
	for k := range map[string]bool{":0": true, ":1": true} {
		mock.QueryOutputs[k+":workspace#N:org:org1"] = &dynamodb.QueryOutput{
			Items: []map[string]types.AttributeValue{item},
			Count: 1,
		}
	}

	claims := newTestClaims("N:user:someone", "N:org:org1", nil)
	store := store_dynamodb.NewAppAccessDatabaseStore(mock, "test-table")

	result := CanAccessApp(context.Background(), claims, app, store)
	assert.True(t, result)
}

func TestCanAccessApp_TeamAccess(t *testing.T) {
	app := &store_dynamodb.AppStoreApplication{
		Uuid:       "app-uuid",
		Visibility: "private",
		OwnerId:    "N:user:other",
	}

	access := store_dynamodb.AppAccess{
		EntityId:   "team#N:team:team1",
		AppId:      "app#app-uuid",
		EntityType: "team",
		AppUuid:    "app-uuid",
		AccessType: "shared",
	}
	item, _ := attributevalue.MarshalMap(access)

	mock := &mockAppAccessTableAPI{
		QueryOutputs: map[string]*dynamodb.QueryOutput{},
	}
	for k := range map[string]bool{":0": true, ":1": true} {
		mock.QueryOutputs[k+":team#N:team:team1"] = &dynamodb.QueryOutput{
			Items: []map[string]types.AttributeValue{item},
			Count: 1,
		}
	}

	teams := []teamUser.Claim{
		{NodeId: "N:team:team1", Name: "Team1", TeamType: "team"},
	}
	claims := newTestClaims("N:user:someone", "N:org:org1", teams)
	store := store_dynamodb.NewAppAccessDatabaseStore(mock, "test-table")

	result := CanAccessApp(context.Background(), claims, app, store)
	assert.True(t, result)
}

func TestIsAppOwner_True(t *testing.T) {
	app := &store_dynamodb.AppStoreApplication{OwnerId: "N:user:owner-123"}
	claims := newTestClaims("N:user:owner-123", "N:org:org1", nil)
	assert.True(t, IsAppOwner(context.Background(), claims, app))
}

func TestIsAppOwner_False(t *testing.T) {
	app := &store_dynamodb.AppStoreApplication{OwnerId: "N:user:owner-123"}
	claims := newTestClaims("N:user:someone-else", "N:org:org1", nil)
	assert.False(t, IsAppOwner(context.Background(), claims, app))
}

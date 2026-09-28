package access

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/pennsieve/app-deploy-service/service/store_dynamodb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	appUuid = "0f3c8a52-6a47-4a8e-9b1a-2d6b8f0c1e11"
	appKey  = "app#" + appUuid
	owner   = "N:user:owner"
	viewer  = "N:user:viewer"
)

type fakeGrants struct {
	grants map[string]bool  // entityId|appId -> granted
	errs   map[string]error // entityId -> error
	calls  []string
}

func (f *fakeGrants) GetAccess(_ context.Context, entityId string, appId string) (*store_dynamodb.AppAccess, error) {
	f.calls = append(f.calls, entityId)
	if err, ok := f.errs[entityId]; ok {
		return nil, err
	}
	if f.grants[entityId+"|"+appId] {
		return &store_dynamodb.AppAccess{EntityId: entityId, AppId: appId}, nil
	}
	return nil, nil
}

type fakeApps struct {
	apps map[string]*store_dynamodb.AppStoreApplication
	err  error
}

func (f *fakeApps) GetById(_ context.Context, uuid string) (*store_dynamodb.AppStoreApplication, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.apps[uuid], nil
}

func privateApp() *store_dynamodb.AppStoreApplication {
	return &store_dynamodb.AppStoreApplication{Uuid: appUuid, Visibility: "private", OwnerId: owner}
}

func newChecker(app *store_dynamodb.AppStoreApplication, grants *fakeGrants) *Checker {
	apps := &fakeApps{apps: map[string]*store_dynamodb.AppStoreApplication{}}
	if app != nil {
		apps.apps[app.Uuid] = app
	}
	return NewChecker(apps, grants)
}

func request(user string, workspaces, teams []string) CheckRequest {
	return CheckRequest{AppUuid: appUuid, UserNodeId: user, WorkspaceNodeIds: workspaces, TeamNodeIds: teams}
}

func TestCheck(t *testing.T) {
	workspaces := []string{"N:organization:ws1", "N:organization:ws2", "N:organization:ws3"}
	teams := []string{"N:team:t1", "N:team:t2"}

	tests := []struct {
		name   string
		app    *store_dynamodb.AppStoreApplication
		grants map[string]bool
		want   bool
	}{
		{
			name: "public app",
			app:  &store_dynamodb.AppStoreApplication{Uuid: appUuid, Visibility: "public", OwnerId: owner},
			want: true,
		},
		{
			name: "owner",
			app:  &store_dynamodb.AppStoreApplication{Uuid: appUuid, Visibility: "private", OwnerId: viewer},
			want: true,
		},
		{
			name:   "user grant",
			app:    privateApp(),
			grants: map[string]bool{"user#" + viewer + "|" + appKey: true},
			want:   true,
		},
		{
			name:   "workspace grant among several",
			app:    privateApp(),
			grants: map[string]bool{"workspace#N:organization:ws2|" + appKey: true},
			want:   true,
		},
		{
			name:   "team grant",
			app:    privateApp(),
			grants: map[string]bool{"team#N:team:t2|" + appKey: true},
			want:   true,
		},
		{
			name:   "grant on a different app",
			app:    privateApp(),
			grants: map[string]bool{"user#" + viewer + "|app#other-app": true},
			want:   false,
		},
		{
			name: "no grant",
			app:  privateApp(),
			want: false,
		},
		{
			name: "unknown app",
			app:  nil,
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			grants := &fakeGrants{grants: tt.grants}
			resp, err := newChecker(tt.app, grants).Check(context.Background(), request(viewer, workspaces, teams))
			require.NoError(t, err)
			assert.Equal(t, tt.want, resp.HasAccess)
		})
	}
}

func TestCheckLooksUpEveryEntityWhenNothingMatches(t *testing.T) {
	grants := &fakeGrants{}
	_, err := newChecker(privateApp(), grants).Check(context.Background(),
		request(viewer, []string{"N:organization:ws1", "N:organization:ws2"}, []string{"N:team:t1"}))
	require.NoError(t, err)
	assert.Equal(t, []string{
		"user#" + viewer,
		"workspace#N:organization:ws1",
		"workspace#N:organization:ws2",
		"team#N:team:t1",
	}, grants.calls)
}

func TestCheckEmptyListsOnlyChecksUser(t *testing.T) {
	grants := &fakeGrants{}
	resp, err := newChecker(privateApp(), grants).Check(context.Background(), request(viewer, []string{}, []string{}))
	require.NoError(t, err)
	assert.False(t, resp.HasAccess)
	assert.Equal(t, []string{"user#" + viewer}, grants.calls)
}

func TestCheckAppStoreError(t *testing.T) {
	c := NewChecker(&fakeApps{err: errors.New("throttled")}, &fakeGrants{})
	_, err := c.Check(context.Background(), request(viewer, nil, nil))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "throttled")
}

func TestCheckGrantStoreError(t *testing.T) {
	grants := &fakeGrants{errs: map[string]error{"workspace#N:organization:ws1": errors.New("throttled")}}
	_, err := newChecker(privateApp(), grants).Check(context.Background(),
		request(viewer, []string{"N:organization:ws1"}, []string{"N:team:t1"}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "throttled")
	assert.Equal(t, []string{"user#" + viewer, "workspace#N:organization:ws1", "team#N:team:t1"}, grants.calls,
		"a failed lookup does not stop the others")
}

func TestCheckGrantFoundDespiteAnotherLookupFailing(t *testing.T) {
	grants := &fakeGrants{
		errs:   map[string]error{"user#" + viewer: errors.New("throttled")},
		grants: map[string]bool{"team#N:team:t1|" + appKey: true},
	}
	resp, err := newChecker(privateApp(), grants).Check(context.Background(), request(viewer, nil, []string{"N:team:t1"}))
	require.NoError(t, err)
	assert.True(t, resp.HasAccess)
}

func TestCheckMissingIds(t *testing.T) {
	grants := &fakeGrants{}
	c := newChecker(&store_dynamodb.AppStoreApplication{Uuid: appUuid, Visibility: "private", OwnerId: ""}, grants)

	resp, err := c.Check(context.Background(), CheckRequest{AppUuid: appUuid})
	require.NoError(t, err)
	assert.False(t, resp.HasAccess, "an empty user never matches an app with no owner")

	resp, err = c.Check(context.Background(), CheckRequest{UserNodeId: viewer})
	require.NoError(t, err)
	assert.False(t, resp.HasAccess)
	assert.Empty(t, grants.calls)
}

func TestCheckContractJSON(t *testing.T) {
	var req CheckRequest
	require.NoError(t, json.Unmarshal([]byte(`{"appUuid":"a","userNodeId":"N:user:u","workspaceNodeIds":["N:organization:o"],"teamNodeIds":[]}`), &req))
	assert.Equal(t, CheckRequest{AppUuid: "a", UserNodeId: "N:user:u", WorkspaceNodeIds: []string{"N:organization:o"}, TeamNodeIds: []string{}}, req)

	out, err := json.Marshal(CheckResponse{HasAccess: false})
	require.NoError(t, err)
	assert.JSONEq(t, `{"hasAccess":false}`, string(out))
}

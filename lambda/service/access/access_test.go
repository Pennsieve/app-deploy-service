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
	errs map[string]error // uuid -> error
}

func (f *fakeApps) GetById(_ context.Context, uuid string) (*store_dynamodb.AppStoreApplication, error) {
	if f.err != nil {
		return nil, f.err
	}
	if err, ok := f.errs[uuid]; ok {
		return nil, err
	}
	return f.apps[uuid], nil
}

type fakeApplications struct {
	applications map[string]store_dynamodb.Application
	err          error
	calls        int
}

func (f *fakeApplications) GetById(_ context.Context, uuid string) (store_dynamodb.Application, error) {
	f.calls++
	if f.err != nil {
		return store_dynamodb.Application{}, f.err
	}
	return f.applications[uuid], nil
}

type fakeVersions struct {
	versions map[string]*store_dynamodb.AppStoreVersion
	err      error
	calls    int
}

func (f *fakeVersions) GetById(_ context.Context, uuid string) (*store_dynamodb.AppStoreVersion, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return f.versions[uuid], nil
}

func privateApp() *store_dynamodb.AppStoreApplication {
	return &store_dynamodb.AppStoreApplication{Uuid: appUuid, Visibility: "private", OwnerId: owner}
}

func newChecker(app *store_dynamodb.AppStoreApplication, grants *fakeGrants) *Checker {
	apps := &fakeApps{apps: map[string]*store_dynamodb.AppStoreApplication{}}
	if app != nil {
		apps.apps[app.Uuid] = app
	}
	return NewChecker(apps, &fakeApplications{}, &fakeVersions{}, grants)
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
	c := NewChecker(&fakeApps{err: errors.New("throttled")}, &fakeApplications{}, &fakeVersions{}, &fakeGrants{})
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

const (
	computeAppUuid = "5b0e7c1d-3f7a-4c2e-8d9b-6a1f2e3d4c5b"
	versionUuid    = "9d8c7b6a-5f4e-4d3c-2b1a-0f9e8d7c6b5a"
)

func computeApp() store_dynamodb.Application {
	return store_dynamodb.Application{Uuid: computeAppUuid, OrganizationId: "N:organization:ws2", UserId: "N:user:creator"}
}

func fallbackChecker(apps *fakeApps, applications *fakeApplications, versions *fakeVersions, grants *fakeGrants) *Checker {
	if apps.apps == nil {
		apps.apps = map[string]*store_dynamodb.AppStoreApplication{}
	}
	return NewChecker(apps, applications, versions, grants)
}

func TestCheckComputeNodeApplication(t *testing.T) {
	tests := []struct {
		name       string
		user       string
		workspaces []string
		want       bool
	}{
		{"workspace match among several", viewer, []string{"N:organization:ws1", "N:organization:ws2"}, true},
		{"workspace mismatch", viewer, []string{"N:organization:ws1", "N:organization:ws3"}, false},
		{"no workspaces", viewer, []string{}, false},
		{"creator outside the workspace", "N:user:creator", []string{"N:organization:ws1"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			versions := &fakeVersions{}
			grants := &fakeGrants{}
			c := fallbackChecker(&fakeApps{},
				&fakeApplications{applications: map[string]store_dynamodb.Application{computeAppUuid: computeApp()}},
				versions, grants)
			resp, err := c.Check(context.Background(), CheckRequest{AppUuid: computeAppUuid, UserNodeId: tt.user, WorkspaceNodeIds: tt.workspaces, TeamNodeIds: []string{"N:team:t1"}})
			require.NoError(t, err)
			assert.Equal(t, tt.want, resp.HasAccess)
			assert.Zero(t, versions.calls, "versions are not consulted once the application is found")
			assert.Empty(t, grants.calls, "grants do not apply to compute-node applications")
		})
	}
}

func TestCheckComputeNodeApplicationWithoutOrganization(t *testing.T) {
	app := store_dynamodb.Application{Uuid: computeAppUuid}
	c := fallbackChecker(&fakeApps{}, &fakeApplications{applications: map[string]store_dynamodb.Application{computeAppUuid: app}}, &fakeVersions{}, &fakeGrants{})
	resp, err := c.Check(context.Background(), CheckRequest{AppUuid: computeAppUuid, UserNodeId: viewer, WorkspaceNodeIds: []string{""}})
	require.NoError(t, err)
	assert.False(t, resp.HasAccess)
}

func TestCheckAppStoreAppDoesNotConsultOtherTables(t *testing.T) {
	applications := &fakeApplications{}
	versions := &fakeVersions{}
	c := fallbackChecker(&fakeApps{apps: map[string]*store_dynamodb.AppStoreApplication{appUuid: privateApp()}}, applications, versions, &fakeGrants{})
	resp, err := c.Check(context.Background(), request(viewer, nil, nil))
	require.NoError(t, err)
	assert.False(t, resp.HasAccess)
	assert.Zero(t, applications.calls)
	assert.Zero(t, versions.calls)
}

func TestCheckVersionResolvesToAppStoreApp(t *testing.T) {
	versions := &fakeVersions{versions: map[string]*store_dynamodb.AppStoreVersion{versionUuid: {Uuid: versionUuid, ApplicationId: appUuid}}}
	tests := []struct {
		name   string
		app    *store_dynamodb.AppStoreApplication
		grants map[string]bool
		want   bool
	}{
		{"public app", &store_dynamodb.AppStoreApplication{Uuid: appUuid, Visibility: "public", OwnerId: owner}, nil, true},
		{"owner", &store_dynamodb.AppStoreApplication{Uuid: appUuid, Visibility: "private", OwnerId: viewer}, nil, true},
		{"team grant on the application", privateApp(), map[string]bool{"team#N:team:t1|" + appKey: true}, true},
		{"grant on the version id does not count", privateApp(), map[string]bool{"user#" + viewer + "|app#" + versionUuid: true}, false},
		{"no grant", privateApp(), nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := fallbackChecker(&fakeApps{apps: map[string]*store_dynamodb.AppStoreApplication{appUuid: tt.app}},
				&fakeApplications{}, versions, &fakeGrants{grants: tt.grants})
			resp, err := c.Check(context.Background(), CheckRequest{AppUuid: versionUuid, UserNodeId: viewer, WorkspaceNodeIds: []string{}, TeamNodeIds: []string{"N:team:t1"}})
			require.NoError(t, err)
			assert.Equal(t, tt.want, resp.HasAccess)
		})
	}
}

func TestCheckVersionPointingAtMissingApp(t *testing.T) {
	versions := &fakeVersions{versions: map[string]*store_dynamodb.AppStoreVersion{versionUuid: {Uuid: versionUuid, ApplicationId: "gone"}}}
	c := fallbackChecker(&fakeApps{}, &fakeApplications{}, versions, &fakeGrants{})
	resp, err := c.Check(context.Background(), CheckRequest{AppUuid: versionUuid, UserNodeId: viewer})
	require.NoError(t, err)
	assert.False(t, resp.HasAccess)
}

func TestCheckVersionWithoutApplicationId(t *testing.T) {
	apps := &fakeApps{errs: map[string]error{"": errors.New("must not look up an empty id")}}
	versions := &fakeVersions{versions: map[string]*store_dynamodb.AppStoreVersion{versionUuid: {Uuid: versionUuid}}}
	resp, err := fallbackChecker(apps, &fakeApplications{}, versions, &fakeGrants{}).Check(context.Background(), CheckRequest{AppUuid: versionUuid, UserNodeId: viewer})
	require.NoError(t, err)
	assert.False(t, resp.HasAccess)
}

func TestCheckNotFoundAnywhere(t *testing.T) {
	applications := &fakeApplications{}
	versions := &fakeVersions{}
	resp, err := fallbackChecker(&fakeApps{}, applications, versions, &fakeGrants{}).Check(context.Background(), request(viewer, []string{"N:organization:ws1"}, nil))
	require.NoError(t, err)
	assert.False(t, resp.HasAccess)
	assert.Equal(t, 1, applications.calls)
	assert.Equal(t, 1, versions.calls)
}

func TestCheckLookupErrors(t *testing.T) {
	throttled := errors.New("throttled")
	version := map[string]*store_dynamodb.AppStoreVersion{versionUuid: {Uuid: versionUuid, ApplicationId: appUuid}}
	tests := []struct {
		name         string
		appUuid      string
		apps         *fakeApps
		applications *fakeApplications
		versions     *fakeVersions
		grants       *fakeGrants
	}{
		{"appstore applications table", appUuid, &fakeApps{err: throttled}, &fakeApplications{}, &fakeVersions{}, &fakeGrants{}},
		{"applications table", computeAppUuid, &fakeApps{}, &fakeApplications{err: throttled}, &fakeVersions{}, &fakeGrants{}},
		{"versions table", versionUuid, &fakeApps{}, &fakeApplications{}, &fakeVersions{err: throttled}, &fakeGrants{}},
		{"appstore application of a version", versionUuid, &fakeApps{errs: map[string]error{appUuid: throttled}}, &fakeApplications{}, &fakeVersions{versions: version}, &fakeGrants{}},
		{"grants of a version's application", versionUuid,
			&fakeApps{apps: map[string]*store_dynamodb.AppStoreApplication{appUuid: privateApp()}},
			&fakeApplications{}, &fakeVersions{versions: version},
			&fakeGrants{errs: map[string]error{"user#" + viewer: throttled}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := fallbackChecker(tt.apps, tt.applications, tt.versions, tt.grants)
			_, err := c.Check(context.Background(), CheckRequest{AppUuid: tt.appUuid, UserNodeId: viewer})
			require.Error(t, err)
			assert.ErrorIs(t, err, throttled)
		})
	}
}

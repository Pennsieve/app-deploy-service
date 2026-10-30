package handler

import (
	"context"
	"testing"

	"github.com/pennsieve/app-deploy-service/service/models"
	"github.com/pennsieve/app-deploy-service/service/store_dynamodb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeApps struct {
	apps   []store_dynamodb.AppStoreApplication
	params map[string][]store_dynamodb.AppParameter
}

func (f *fakeApps) GetBySourceUrl(context.Context, string) ([]store_dynamodb.AppStoreApplication, error) {
	return nil, nil
}
func (f *fakeApps) GetAll(context.Context) ([]store_dynamodb.AppStoreApplication, error) {
	return f.apps, nil
}
func (f *fakeApps) GetById(context.Context, string) (*store_dynamodb.AppStoreApplication, error) {
	return nil, nil
}
func (f *fakeApps) Insert(context.Context, store_dynamodb.AppStoreApplication) error { return nil }
func (f *fakeApps) UpdateVisibility(context.Context, string, string) error           { return nil }
func (f *fakeApps) UpdateStatus(context.Context, string, models.AppStoreStatus) error {
	return nil
}
func (f *fakeApps) UpdateGithubVisibility(context.Context, string, bool) error { return nil }
func (f *fakeApps) UpdateParams(_ context.Context, uuid string, p []store_dynamodb.AppParameter) error {
	f.params[uuid] = p
	return nil
}
func (f *fakeApps) UpdateSummary(_ context.Context, uuid string, summary string, tag string) error {
	return nil
}

type fakeVersions struct {
	versions  []store_dynamodb.AppStoreVersion
	manifests map[string]*store_dynamodb.AppManifest
}

func (f *fakeVersions) GetById(context.Context, string) (*store_dynamodb.AppStoreVersion, error) {
	return nil, nil
}
func (f *fakeVersions) GetByApplicationId(_ context.Context, id string) ([]store_dynamodb.AppStoreVersion, error) {
	var out []store_dynamodb.AppStoreVersion
	for _, v := range f.versions {
		if v.ApplicationId == id {
			out = append(out, v)
		}
	}
	return out, nil
}
func (f *fakeVersions) GetByApplicationIdAndVersion(context.Context, string, string) ([]store_dynamodb.AppStoreVersion, error) {
	return nil, nil
}
func (f *fakeVersions) Insert(context.Context, store_dynamodb.AppStoreVersion) error { return nil }
func (f *fakeVersions) UpdateStatus(context.Context, string, string) error           { return nil }
func (f *fakeVersions) SetManifest(_ context.Context, uuid string, m *store_dynamodb.AppManifest) error {
	f.manifests[uuid] = m
	return nil
}
func (f *fakeVersions) SetRegistrationDetails(context.Context, string, string, []string) error {
	return nil
}
func (f *fakeVersions) SetCitation(context.Context, string, *store_dynamodb.AppCitation) error {
	return nil
}

func TestBackfillManifests(t *testing.T) {
	apps := &fakeApps{
		apps:   []store_dynamodb.AppStoreApplication{{Uuid: "a", SourceUrl: "https://github.com/o/a"}, {Uuid: "b", SourceUrl: "https://github.com/o/b"}},
		params: map[string][]store_dynamodb.AppParameter{},
	}
	kept := &store_dynamodb.AppManifest{Name: "kept", Parameters: []store_dynamodb.AppParameter{{Name: "old"}}}
	versions := &fakeVersions{
		versions: []store_dynamodb.AppStoreVersion{
			{Uuid: "a1", ApplicationId: "a", Version: "v1.0.0", CreatedAt: "3", Manifest: kept},
			{Uuid: "a2", ApplicationId: "a", Version: "v2.0.0", CreatedAt: "1"},
			{Uuid: "b1", ApplicationId: "b", Version: "v1.0.0", CreatedAt: "1"},
		},
		manifests: map[string]*store_dynamodb.AppManifest{},
	}
	read := func(_ context.Context, sourceUrl, tag string) (*store_dynamodb.AppManifest, bool) {
		if sourceUrl == "https://github.com/o/b" {
			return nil, false // no app.yml
		}
		return &store_dynamodb.AppManifest{Name: tag, Parameters: []store_dynamodb.AppParameter{{Name: "new"}}}, true
	}

	res, err := BackfillManifests(context.Background(), apps, versions, read, false, func(string, ...any) {})
	require.NoError(t, err)
	assert.Equal(t, BackfillResult{Apps: 2, Versions: 3, Stored: 1, WithoutAppYml: 1, ParamsSet: 1}, res)
	assert.Equal(t, "v2.0.0", versions.manifests["a2"].Name, "the version without one gets its manifest")
	assert.NotContains(t, versions.manifests, "a1", "a stored manifest is kept")
	assert.Equal(t, "new", apps.params["a"][0].Name, "the app's params come from its newest version")
	assert.NotContains(t, apps.params, "b", "an app without app.yml keeps its params")

	// A dry run writes nothing.
	versions.manifests, apps.params = map[string]*store_dynamodb.AppManifest{}, map[string][]store_dynamodb.AppParameter{}
	_, err = BackfillManifests(context.Background(), apps, versions, read, true, func(string, ...any) {})
	require.NoError(t, err)
	assert.Empty(t, versions.manifests)
	assert.Empty(t, apps.params)
}

package handler

import (
	"testing"

	"github.com/pennsieve/app-deploy-service/service/models"
	"github.com/stretchr/testify/assert"
)

func TestInitialVisibility(t *testing.T) {
	assert.Equal(t, "public", initialVisibility(false, nil), "public repo, no sharing")
	assert.Equal(t, "private", initialVisibility(true, nil), "private repo, no sharing")
	assert.Equal(t, "private", initialVisibility(false, &models.SetPermissionsRequest{Visibility: "private"}), "sharing wins over the repo")
	assert.Equal(t, "public", initialVisibility(true, &models.SetPermissionsRequest{Visibility: "public"}), "sharing wins over the repo")
	assert.Equal(t, "private", initialVisibility(true, &models.SetPermissionsRequest{Visibility: "friends"}), "an invalid value falls back to the repo")
}

func TestAccessEntriesFor(t *testing.T) {
	req := models.SetPermissionsRequest{
		Visibility: "private",
		Users:      []models.PermissionEntity{{EntityId: "N:user:owner"}, {EntityId: "N:user:friend", OrganizationId: "N:organization:a"}},
		Teams:      []models.PermissionEntity{{EntityId: "N:team:t", OrganizationId: "N:organization:a"}},
		Workspaces: []models.PermissionEntity{{EntityId: "N:organization:a", OrganizationId: "N:organization:a"}},
	}

	entries := accessEntriesFor("app-1", "N:user:owner", "N:user:owner", "now", req)

	assert.Len(t, entries, 4, "owner + friend + team + workspace; the owner listed as a user is not duplicated")
	assert.Equal(t, "user#N:user:owner", entries[0].EntityId)
	assert.Equal(t, "owner", entries[0].AccessType)
	assert.Equal(t, "app#app-1", entries[0].AppId)

	byEntity := map[string]string{}
	for _, e := range entries {
		byEntity[e.EntityId] = e.AccessType
	}
	assert.Equal(t, "shared", byEntity["user#N:user:friend"])
	assert.Equal(t, "shared", byEntity["team#N:team:t"])
	assert.Equal(t, "workspace", byEntity["workspace#N:organization:a"])

	ownerOnly := accessEntriesFor("app-1", "N:user:owner", "N:user:owner", "now", models.SetPermissionsRequest{})
	assert.Len(t, ownerOnly, 1, "no sharing gives the owner row alone")
}

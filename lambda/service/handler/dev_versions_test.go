package handler

import (
	"testing"

	"github.com/pennsieve/app-deploy-service/service/models"
	"github.com/stretchr/testify/assert"
)

func TestVisibleVersions(t *testing.T) {
	release := models.AppStoreVersion{Uuid: "r", Version: "v1.0.0"}
	dev := models.AppStoreVersion{Uuid: "d", Version: "dev-main", Channel: models.ChannelDev}
	versions := []models.AppStoreVersion{release, dev}

	assert.Equal(t, versions, visibleVersions(versions, true))
	assert.Equal(t, []models.AppStoreVersion{release}, visibleVersions(versions, false))
	assert.Empty(t, visibleVersions([]models.AppStoreVersion{dev}, false))
	assert.Empty(t, visibleVersions(nil, false))
}

func TestLatestVersionTagSkipsDevBuilds(t *testing.T) {
	versions := []models.AppStoreVersion{
		{Version: "v1.0.0", CreatedAt: "2026-01-01"},
		{Version: "v1.1.0", CreatedAt: "2026-02-01"},
		{Version: "dev-main", Channel: models.ChannelDev, CreatedAt: "2026-03-01"},
	}
	assert.Equal(t, "v1.1.0", latestVersionTag(versions))

	onlyDev := []models.AppStoreVersion{{Version: "dev-main", Channel: models.ChannelDev, CreatedAt: "2026-03-01"}}
	assert.Equal(t, "", latestVersionTag(onlyDev))
}

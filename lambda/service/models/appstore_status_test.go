package models

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAppStoreStatus_UnmarshalJSON_Active(t *testing.T) {
	var s AppStoreStatus
	require.NoError(t, json.Unmarshal([]byte(`"active"`), &s))
	assert.Equal(t, AppStoreStatusActive, s)
}

func TestAppStoreStatus_UnmarshalJSON_Archived(t *testing.T) {
	var s AppStoreStatus
	require.NoError(t, json.Unmarshal([]byte(`"archived"`), &s))
	assert.Equal(t, AppStoreStatusArchived, s)
}

func TestAppStoreStatus_UnmarshalJSON_Invalid(t *testing.T) {
	var s AppStoreStatus
	err := json.Unmarshal([]byte(`"foo"`), &s)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid AppStoreStatus")
}

func TestAppStoreStatus_UnmarshalJSON_Empty(t *testing.T) {
	var s AppStoreStatus
	err := json.Unmarshal([]byte(`""`), &s)
	require.Error(t, err)
}

func TestAppStoreStatus_UnmarshalJSON_NonString(t *testing.T) {
	var s AppStoreStatus
	err := json.Unmarshal([]byte(`123`), &s)
	require.Error(t, err)
}

func TestPatchAppStoreApplicationRequest_UnmarshalJSON_RejectsInvalidStatus(t *testing.T) {
	var req PatchAppStoreApplicationRequest
	err := json.Unmarshal([]byte(`{"status":"deleted"}`), &req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid AppStoreStatus")
}

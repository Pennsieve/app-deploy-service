package models

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAppParameter_MarshalJSON_RequiredTrue(t *testing.T) {
	b, err := json.Marshal(AppParameter{Name: "CONSENT_GROUP", Type: "string", Required: true})
	require.NoError(t, err)
	assert.JSONEq(t, `{"name":"CONSENT_GROUP","type":"string","required":true}`, string(b))
}

func TestAppParameter_MarshalJSON_RequiredFalseAlwaysEmitted(t *testing.T) {
	b, err := json.Marshal(AppParameter{Name: "PHS_ACCESSION", Type: "string"})
	require.NoError(t, err)
	assert.JSONEq(t, `{"name":"PHS_ACCESSION","type":"string","required":false}`, string(b))
}

func TestAppParameter_UnmarshalJSON_MissingRequiredDefaultsFalse(t *testing.T) {
	var p AppParameter
	require.NoError(t, json.Unmarshal([]byte(`{"name":"IS_TUMOR","defaultValue":"No"}`), &p))
	assert.False(t, p.Required)
	assert.Equal(t, "No", p.DefaultValue)
}

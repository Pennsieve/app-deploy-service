package handler

import (
	"net/http"
	"testing"

	"github.com/pennsieve/app-deploy-service/service/store_dynamodb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const dynasdImage = "123.dkr.ecr.us-east-1.amazonaws.com/appstore-private:4242-v0.3.0"

// dynasdVersion is DynaSD v0.3.0 with a cpu (default) and a gpu variant.
func dynasdVersion(cpu, gpu store_dynamodb.AppVariant) store_dynamodb.AppStoreVersion {
	return store_dynamodb.AppStoreVersion{
		Uuid: "ver", Version: "v0.3.0", Status: "deployed", DestinationUrl: dynasdImage,
		DefaultVariant: "cpu", VariantOrder: []string{"cpu", "gpu"},
		Variants: map[string]store_dynamodb.AppVariant{"cpu": cpu, "gpu": gpu},
	}
}

func TestRegistryDestination_NoVariant(t *testing.T) {
	// an app without variants: exactly today's behaviour
	plain := store_dynamodb.AppStoreVersion{Version: "v1.0.0", Status: "deployed", DestinationUrl: "repo:1-v1.0.0"}
	url, refusal := registryDestination(plain, "")
	require.Nil(t, refusal)
	assert.Equal(t, "repo:1-v1.0.0", url)

	for _, v := range []store_dynamodb.AppStoreVersion{
		{Version: "v1.0.0", Status: "registering", DestinationUrl: "repo:1-v1.0.0"},
		{Version: "v1.0.0", Status: "error", DestinationUrl: "repo:1-v1.0.0"},
		{Version: "v1.0.0", Status: "deployed"},
	} {
		_, refusal = registryDestination(v, "")
		require.NotNil(t, refusal)
		assert.Equal(t, registryRefusal{http.StatusNotFound, "version is not yet deployed"}, *refusal)
	}

	// an app with variants, no variant asked: the version's own image (the default's)
	url, refusal = registryDestination(dynasdVersion(store_dynamodb.AppVariant{}, store_dynamodb.AppVariant{}), "")
	require.Nil(t, refusal)
	assert.Equal(t, dynasdImage, url)
}

func TestRegistryDestination_VariantOnVersionWithout(t *testing.T) {
	plain := store_dynamodb.AppStoreVersion{Version: "v0.2.2", Status: "deployed", DestinationUrl: "repo:1-v0.2.2"}
	_, refusal := registryDestination(plain, "gpu")
	require.NotNil(t, refusal)
	assert.Equal(t, registryRefusal{http.StatusNotFound, "version v0.2.2 has no variants"}, *refusal)
}

func TestRegistryDestination_UnknownVariant(t *testing.T) {
	_, refusal := registryDestination(dynasdVersion(store_dynamodb.AppVariant{}, store_dynamodb.AppVariant{}), "nope")
	require.NotNil(t, refusal)
	assert.Equal(t, registryRefusal{http.StatusNotFound, "variant nope not found for this version"}, *refusal)
}

func TestRegistryDestination_DefaultVariant(t *testing.T) {
	// no per-variant build recorded: the default follows the version
	url, refusal := registryDestination(dynasdVersion(store_dynamodb.AppVariant{}, store_dynamodb.AppVariant{}), "cpu")
	require.Nil(t, refusal)
	assert.Equal(t, dynasdImage, url)

	v := dynasdVersion(store_dynamodb.AppVariant{}, store_dynamodb.AppVariant{})
	v.Status = "registering"
	_, refusal = registryDestination(v, "cpu")
	require.NotNil(t, refusal)
	assert.Equal(t, "variant cpu of v0.3.0 is not yet deployed", refusal.Message)

	v.Status = "error"
	_, refusal = registryDestination(v, "cpu")
	require.NotNil(t, refusal)
	assert.Equal(t, "variant cpu of v0.3.0 failed to build", refusal.Message)

	// its own status wins once recorded
	v = dynasdVersion(store_dynamodb.AppVariant{Status: "deployed", DestinationUrl: dynasdImage}, store_dynamodb.AppVariant{})
	url, refusal = registryDestination(v, "cpu")
	require.Nil(t, refusal)
	assert.Equal(t, dynasdImage, url)
}

func TestRegistryDestination_OtherVariant(t *testing.T) {
	// no status yet (builds per variant not recorded): not deployed
	_, refusal := registryDestination(dynasdVersion(store_dynamodb.AppVariant{}, store_dynamodb.AppVariant{}), "gpu")
	require.NotNil(t, refusal)
	assert.Equal(t, registryRefusal{http.StatusNotFound, "variant gpu of v0.3.0 is not yet deployed"}, *refusal)

	_, refusal = registryDestination(dynasdVersion(store_dynamodb.AppVariant{}, store_dynamodb.AppVariant{Status: "registering"}), "gpu")
	require.NotNil(t, refusal)
	assert.Equal(t, "variant gpu of v0.3.0 is not yet deployed", refusal.Message)

	_, refusal = registryDestination(dynasdVersion(store_dynamodb.AppVariant{}, store_dynamodb.AppVariant{Status: "error", FailureReason: "no stage gpu"}), "gpu")
	require.NotNil(t, refusal)
	assert.Equal(t, registryRefusal{http.StatusNotFound, "variant gpu of v0.3.0 failed to build"}, *refusal)

	// deployed: its own image, or the default's tag with --<id>
	url, refusal := registryDestination(dynasdVersion(store_dynamodb.AppVariant{}, store_dynamodb.AppVariant{Status: "deployed"}), "gpu")
	require.Nil(t, refusal)
	assert.Equal(t, dynasdImage+"--gpu", url)

	url, refusal = registryDestination(dynasdVersion(store_dynamodb.AppVariant{}, store_dynamodb.AppVariant{Status: "deployed", DestinationUrl: "repo:x--gpu"}), "gpu")
	require.Nil(t, refusal)
	assert.Equal(t, "repo:x--gpu", url)

	// a failed non-default variant leaves the version and its default usable
	v := dynasdVersion(store_dynamodb.AppVariant{}, store_dynamodb.AppVariant{Status: "error"})
	url, refusal = registryDestination(v, "")
	require.Nil(t, refusal)
	assert.Equal(t, dynasdImage, url)
	url, refusal = registryDestination(v, "cpu")
	require.Nil(t, refusal)
	assert.Equal(t, dynasdImage, url)

	// a non-default variant deployed while the default failed is still authorized
	v = dynasdVersion(store_dynamodb.AppVariant{Status: "error"}, store_dynamodb.AppVariant{Status: "deployed"})
	v.Status = "error"
	url, refusal = registryDestination(v, "gpu")
	require.Nil(t, refusal)
	assert.Equal(t, dynasdImage+"--gpu", url)
}

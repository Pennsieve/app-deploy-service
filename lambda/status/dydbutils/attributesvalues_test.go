package dydbutils_test

import (
	"github.com/google/uuid"
	"github.com/pennsieve/app-deploy-service/status/dydbutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
)

type TestStruct struct {
	ID    string `dynamodbav:"id"`
	IntID int    `dynamodbav:"intId"`
}

func TestFromItem_NilItem(t *testing.T) {
	tstStruct, err := dydbutils.FromItem[TestStruct](nil)
	require.NoError(t, err)
	assert.Nil(t, tstStruct)
}

func TestRoundTrip(t *testing.T) {
	tstStruct := TestStruct{
		ID:    uuid.NewString(),
		IntID: 78,
	}
	item, err := dydbutils.ItemImpl(tstStruct)
	require.NoError(t, err)
	assert.Len(t, item, 2)
	assert.Contains(t, item, "id")
	assert.Contains(t, item, "intId")

	unmarshalled, err := dydbutils.FromItem[TestStruct](item)
	require.NoError(t, err)
	if assert.NotNil(t, unmarshalled) {
		assert.Equal(t, tstStruct, *unmarshalled)
	}

}

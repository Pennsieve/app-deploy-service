package lambdaeligibility

import (
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var checked = time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)

func lambdaImage() ImageFacts {
	return ImageFacts{OS: "linux", Architecture: "amd64", Labels: map[string]string{ImageLabel: "1.0.0"}, CompressedBytes: 2_000_000_000}
}

func ptr[T any](v T) *T { return &v }

func TestDecide(t *testing.T) {
	cases := []struct {
		name string
		rt   Runtime
		img  ImageFacts
		refs bool
		want []string
	}{
		{name: "a standard app with a Lambda image", rt: Runtime{ComputeTypes: []string{"standard"}}, img: lambdaImage()},
		{name: "nothing declared", img: lambdaImage()},
		{name: "optimize: true", rt: Runtime{Optimize: ptr(true)}, img: lambdaImage()},
		{name: "at the limits", rt: Runtime{Memory: 10240, CPU: 6144, TimeoutSeconds: 900, ComputeTypes: []string{"standard", "gpu"}}, img: lambdaImage()},
		{name: "an image of exactly 10 GB", img: func() ImageFacts { i := lambdaImage(); i.CompressedBytes = MaxImageBytes; return i }()},
		{name: "opted out", rt: Runtime{Optimize: ptr(false)}, img: lambdaImage(),
			want: []string{"the app opts out of optimized runtimes (runtime.optimize: false)"}},
		{name: "GPU only", rt: Runtime{ComputeTypes: []string{"gpu"}}, img: lambdaImage(),
			want: []string{"it runs on GPU only"}},
		{name: "interactive", rt: Runtime{Interactive: true, SessionType: "jupyter"}, img: lambdaImage(),
			want: []string{"interactive sessions don't run on Lambda"}},
		{name: "reference inputs", img: lambdaImage(), refs: true,
			want: []string{"it reads reference data, which a Lambda step can't"}},
		{name: "too big to run", rt: Runtime{Memory: 16384, CPU: 6656, TimeoutSeconds: 7200}, img: lambdaImage(),
			want: []string{
				"it needs 16 GB of memory; a Lambda step has at most 10 GB",
				"it needs 6.5 vCPU; a Lambda step has at most 6",
				"it may run up to 2 hours; a Lambda step runs at most 15 minutes",
			}},
		{name: "a 15 minute timeout fits", rt: Runtime{TimeoutSeconds: 900}, img: lambdaImage()},
		{name: "a 16 minute timeout", rt: Runtime{TimeoutSeconds: 960}, img: lambdaImage(),
			want: []string{"it may run up to 16 minutes; a Lambda step runs at most 15 minutes"}},
		{name: "an image built before the runtime, too big, for arm64",
			img: ImageFacts{OS: "linux", Architecture: "arm64", CompressedBytes: 12_340_000_000},
			want: []string{
				"the image is 12.3 GB; Lambda takes images up to 10 GB",
				"the image has no Lambda step runtime: rebuild the app",
				"the image is not linux/amd64",
			}},
		{name: "a windows image", img: func() ImageFacts { i := lambdaImage(); i.OS = "windows"; return i }(),
			want: []string{"the image is not linux/amd64"}},
		{name: "everything at once", rt: Runtime{Optimize: ptr(false), ComputeTypes: []string{"gpu"}, Interactive: true}, img: ImageFacts{}, refs: true,
			want: []string{
				"the app opts out of optimized runtimes (runtime.optimize: false)",
				"it runs on GPU only",
				"interactive sessions don't run on Lambda",
				"it reads reference data, which a Lambda step can't",
				"the image has no Lambda step runtime: rebuild the app",
				"the image is not linux/amd64",
			}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Decide(c.rt, c.img, c.refs, checked)
			assert.Equal(t, c.want, got.Reasons)
			assert.Equal(t, len(c.want) == 0, got.Eligible)
			assert.Equal(t, c.img.CompressedBytes, got.ImageBytes)
			assert.Equal(t, "2026-11-03T12:00:00Z", got.CheckedAt)
		})
	}
}

// The record's attribute names are what the API and workflow-service read.
func TestEligibility_Stored(t *testing.T) {
	av, err := attributevalue.Marshal(Eligibility{Eligible: false, Reasons: []string{"it runs on GPU only"}, ImageBytes: 42, CheckedAt: "2026-11-03T12:00:00Z"})
	require.NoError(t, err)
	m := av.(*types.AttributeValueMemberM).Value
	assert.ElementsMatch(t, []string{"eligible", "reasons", "imageBytes", "checkedAt"}, keys(m))

	av, err = attributevalue.Marshal(Eligibility{Eligible: true, CheckedAt: "2026-11-03T12:00:00Z"})
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"eligible", "checkedAt"}, keys(av.(*types.AttributeValueMemberM).Value))
}

func keys(m map[string]types.AttributeValue) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestManifest_HasReferenceInputs(t *testing.T) {
	item := map[string]types.AttributeValue{
		"runtime": &types.AttributeValueMemberM{Value: map[string]types.AttributeValue{
			"memory":   &types.AttributeValueMemberN{Value: "2048"},
			"optimize": &types.AttributeValueMemberBOOL{Value: false},
		}},
		"inputs": &types.AttributeValueMemberL{Value: []types.AttributeValue{
			&types.AttributeValueMemberM{Value: map[string]types.AttributeValue{"name": &types.AttributeValueMemberS{Value: "data"}}},
		}},
	}
	var m Manifest
	require.NoError(t, attributevalue.UnmarshalMap(item, &m))
	assert.False(t, m.HasReferenceInputs())
	assert.Equal(t, 2048, m.Runtime.Memory)
	require.NotNil(t, m.Runtime.Optimize)
	assert.False(t, *m.Runtime.Optimize)

	item["inputs"].(*types.AttributeValueMemberL).Value = append(item["inputs"].(*types.AttributeValueMemberL).Value,
		&types.AttributeValueMemberM{Value: map[string]types.AttributeValue{
			"name":      &types.AttributeValueMemberS{Value: "genome"},
			"reference": &types.AttributeValueMemberM{Value: map[string]types.AttributeValue{"kind": &types.AttributeValueMemberS{Value: "genome"}}},
		}})
	m = Manifest{}
	require.NoError(t, attributevalue.UnmarshalMap(item, &m))
	assert.True(t, m.HasReferenceInputs())

	var none *Manifest
	assert.False(t, none.HasReferenceInputs())
}

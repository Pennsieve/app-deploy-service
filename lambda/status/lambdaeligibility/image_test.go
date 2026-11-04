package lambdaeligibility

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ecr"
	ecrtypes "github.com/aws/aws-sdk-go-v2/service/ecr/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeRegistry is an ECR with some manifests (by tag or digest) and config
// blobs (by digest) served from an httptest server.
type fakeRegistry struct {
	manifests map[string][2]string // tag or digest → media type, manifest
	server    *httptest.Server
	gets      []*ecr.BatchGetImageInput
}

func newFakeRegistry(t *testing.T, configs map[string]string) *fakeRegistry {
	r := &fakeRegistry{manifests: map[string][2]string{}}
	r.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		blob, ok := configs[req.URL.Path[1:]]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(blob))
	}))
	t.Cleanup(r.server.Close)
	return r
}

func (r *fakeRegistry) BatchGetImage(_ context.Context, in *ecr.BatchGetImageInput, _ ...func(*ecr.Options)) (*ecr.BatchGetImageOutput, error) {
	r.gets = append(r.gets, in)
	key := aws.ToString(in.ImageIds[0].ImageTag) + aws.ToString(in.ImageIds[0].ImageDigest)
	m, ok := r.manifests[key]
	if !ok {
		return &ecr.BatchGetImageOutput{Failures: []ecrtypes.ImageFailure{{FailureCode: ecrtypes.ImageFailureCodeImageNotFound}}}, nil
	}
	return &ecr.BatchGetImageOutput{Images: []ecrtypes.Image{{ImageManifest: aws.String(m[1]), ImageManifestMediaType: aws.String(m[0])}}}, nil
}

func (r *fakeRegistry) GetDownloadUrlForLayer(_ context.Context, in *ecr.GetDownloadUrlForLayerInput, _ ...func(*ecr.Options)) (*ecr.GetDownloadUrlForLayerOutput, error) {
	return &ecr.GetDownloadUrlForLayerOutput{DownloadUrl: aws.String(r.server.URL + "/" + aws.ToString(in.LayerDigest))}, nil
}

const (
	testImage   = "123456789012.dkr.ecr.us-east-1.amazonaws.com/dev-app-store-use1:5f1c0a-v1.0.0"
	ociManifest = "application/vnd.oci.image.manifest.v1+json"
	ociIndex    = "application/vnd.oci.image.index.v1+json"
)

func TestReadImage_SingleManifest(t *testing.T) {
	r := newFakeRegistry(t, map[string]string{
		"sha256:config": `{"os":"linux","architecture":"amd64","config":{"Labels":{"net.pennsieve.lambda-step-runtime":"1.2.0"}}}`,
	})
	r.manifests["5f1c0a-v1.0.0"] = [2]string{ociManifest, `{"config":{"digest":"sha256:config"},"layers":[{"size":100},{"size":250}]}`}

	facts, err := ReadImage(context.Background(), r, r.server.Client(), testImage)
	require.NoError(t, err)
	assert.Equal(t, ImageFacts{OS: "linux", Architecture: "amd64", Labels: map[string]string{ImageLabel: "1.2.0"}, CompressedBytes: 350}, facts)
	require.Len(t, r.gets, 1)
	assert.Equal(t, "123456789012", aws.ToString(r.gets[0].RegistryId))
	assert.Equal(t, "dev-app-store-use1", aws.ToString(r.gets[0].RepositoryName))
	assert.Contains(t, r.gets[0].AcceptedMediaTypes, ociIndex)
}

func TestReadImage_IndexPicksAmd64(t *testing.T) {
	r := newFakeRegistry(t, map[string]string{
		"sha256:amd-config": `{"os":"linux","architecture":"amd64","config":{"Labels":{"net.pennsieve.lambda-step-runtime":"1.2.0"}}}`,
	})
	r.manifests["5f1c0a-v1.0.0"] = [2]string{ociIndex, `{"manifests":[
		{"digest":"sha256:attest","platform":{"os":"unknown","architecture":"unknown"}},
		{"digest":"sha256:arm","platform":{"os":"linux","architecture":"arm64"}},
		{"digest":"sha256:amd","platform":{"os":"linux","architecture":"amd64"}}]}`}
	r.manifests["sha256:amd"] = [2]string{"", `{"mediaType":"` + ociManifest + `","config":{"digest":"sha256:amd-config"},"layers":[{"size":7}]}`}

	facts, err := ReadImage(context.Background(), r, r.server.Client(), testImage)
	require.NoError(t, err)
	assert.Equal(t, "amd64", facts.Architecture)
	assert.Equal(t, int64(7), facts.CompressedBytes)
}

func TestReadImage_IndexWithoutAmd64(t *testing.T) {
	r := newFakeRegistry(t, map[string]string{
		"sha256:arm-config": `{"os":"linux","architecture":"arm64","config":{}}`,
	})
	r.manifests["5f1c0a-v1.0.0"] = [2]string{ociIndex, `{"manifests":[
		{"digest":"sha256:attest","platform":{"os":"unknown","architecture":"unknown"}},
		{"digest":"sha256:arm","platform":{"os":"linux","architecture":"arm64"}}]}`}
	r.manifests["sha256:arm"] = [2]string{ociManifest, `{"config":{"digest":"sha256:arm-config"},"layers":[{"size":7}]}`}

	facts, err := ReadImage(context.Background(), r, r.server.Client(), testImage)
	require.NoError(t, err)
	assert.Equal(t, "arm64", facts.Architecture)
	assert.Contains(t, Decide(Runtime{}, facts, checked).Reasons, "the image is not linux/amd64")
}

func TestReadImage_Errors(t *testing.T) {
	r := newFakeRegistry(t, nil)
	_, err := ReadImage(context.Background(), r, r.server.Client(), "docker.io/library/python:3.12")
	assert.ErrorContains(t, err, "not an image in ECR")

	_, err = ReadImage(context.Background(), r, r.server.Client(), testImage)
	assert.ErrorContains(t, err, "not in ECR")

	r.manifests["5f1c0a-v1.0.0"] = [2]string{ociManifest, `{"config":{"digest":"sha256:gone"},"layers":[]}`}
	_, err = ReadImage(context.Background(), r, r.server.Client(), testImage)
	assert.ErrorContains(t, err, "HTTP 404")
}

func TestParseECRImage(t *testing.T) {
	registry, repo, id, ok := ParseECRImage("123456789012.dkr.ecr.us-east-1.amazonaws.com/repo@sha256:abc")
	require.True(t, ok)
	assert.Equal(t, "123456789012", registry)
	assert.Equal(t, "repo", repo)
	assert.Equal(t, "sha256:abc", aws.ToString(id.ImageDigest))
	assert.Nil(t, id.ImageTag)
}

package handler

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ecr"
	ecrtypes "github.com/aws/aws-sdk-go-v2/service/ecr/types"
)

// fakeECR returns a canned manifest and records the tags it was asked for.
type fakeECR struct {
	manifest      string // returned as the image manifest when set
	err           error  // returned for every call when set
	requestedTags []string
}

func (f *fakeECR) BatchGetImage(_ context.Context, in *ecr.BatchGetImageInput, _ ...func(*ecr.Options)) (*ecr.BatchGetImageOutput, error) {
	tag := ""
	if len(in.ImageIds) > 0 && in.ImageIds[0].ImageTag != nil {
		tag = *in.ImageIds[0].ImageTag
	}
	f.requestedTags = append(f.requestedTags, tag)
	if f.err != nil {
		return nil, f.err
	}
	if f.manifest == "" {
		return &ecr.BatchGetImageOutput{}, nil // no images found
	}
	return &ecr.BatchGetImageOutput{
		Images: []ecrtypes.Image{{ImageManifest: aws.String(f.manifest)}},
	}, nil
}

const sociIndexManifest = `{
	"mediaType":"application/vnd.oci.image.index.v1+json",
	"manifests":[
		{"mediaType":"application/vnd.oci.image.manifest.v1+json","digest":"sha256:127d886","annotations":{"com.amazon.soci.index-digest":"sha256:b75ee60"}},
		{"mediaType":"application/vnd.oci.image.manifest.v1+json","artifactType":"application/vnd.amazon.soci.index.v2+json","digest":"sha256:b75ee60","annotations":{"com.amazon.soci.image-manifest-digest":"sha256:127d886"}}
	]
}`

func TestResolveAppStoreImageURL(t *testing.T) {
	const base = "941165240011.dkr.ecr.us-east-1.amazonaws.com/dev-appstore-private-use1"
	const plainTag = base + ":4101059555-v0.1.3"

	t.Run("soci index present -> soci manifest digest", func(t *testing.T) {
		f := &fakeECR{manifest: sociIndexManifest}
		got := resolveAppStoreImageURL(context.Background(), f, plainTag)
		want := base + "@sha256:127d886"
		if got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
		if len(f.requestedTags) != 1 || f.requestedTags[0] != "4101059555-v0.1.3-soci" {
			t.Fatalf("expected lookup of -soci tag, got %v", f.requestedTags)
		}
	})

	t.Run("no soci index -> original tag unchanged", func(t *testing.T) {
		f := &fakeECR{} // no images
		if got := resolveAppStoreImageURL(context.Background(), f, plainTag); got != plainTag {
			t.Fatalf("got %q, want unchanged %q", got, plainTag)
		}
	})

	t.Run("ecr error -> original tag unchanged", func(t *testing.T) {
		f := &fakeECR{err: errors.New("boom")}
		if got := resolveAppStoreImageURL(context.Background(), f, plainTag); got != plainTag {
			t.Fatalf("got %q, want unchanged %q", got, plainTag)
		}
	})

	t.Run("non-ecr / digest / untagged passthrough (no ECR call)", func(t *testing.T) {
		for _, in := range []string{
			"pennsieve/session-proxy:v0.1.0",
			"docker.io/pennsieve/session-proxy:v0.1.0",
			base + "@sha256:deadbeef",
			"python:3.12-slim",
		} {
			f := &fakeECR{manifest: sociIndexManifest}
			if got := resolveAppStoreImageURL(context.Background(), f, in); got != in {
				t.Errorf("resolveAppStoreImageURL(%q) = %q, want unchanged", in, got)
			}
			if len(f.requestedTags) != 0 {
				t.Errorf("expected no ECR call for %q, got %v", in, f.requestedTags)
			}
		}
	})
}

func TestSociImageManifestDigest(t *testing.T) {
	cases := []struct{ name, manifest, want string }{
		{"preferred ztoc annotation", sociIndexManifest, "sha256:127d886"},
		{"fallback index-digest", `{"manifests":[{"digest":"sha256:abc","annotations":{"com.amazon.soci.index-digest":"sha256:zzz"}}]}`, "sha256:abc"},
		{"no soci children", `{"manifests":[{"digest":"sha256:abc"}]}`, ""},
		{"invalid json", `nope`, ""},
		{"empty", `{}`, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := sociImageManifestDigest(c.manifest); got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

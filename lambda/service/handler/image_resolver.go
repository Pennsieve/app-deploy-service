package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ecr"
	ecrtypes "github.com/aws/aws-sdk-go-v2/service/ecr/types"
)

// SOCI v2 index annotations, as produced by the AWS SOCI Index Builder.
const (
	// sociIndexArtifactType is the artifactType of the SOCI v2 ztoc artifact
	// stored inside the "<tag>-soci" OCI image index.
	sociIndexArtifactType = "application/vnd.amazon.soci.index.v2+json"
	// sociImageManifestDigestAnnotation, on the ztoc artifact, names the digest
	// of the soci-enabled image manifest (the runnable clone of the image
	// carrying the index-digest link) that must be deployed for Fargate to
	// lazy-load.
	sociImageManifestDigestAnnotation = "com.amazon.soci.image-manifest-digest"
	// sociIndexDigestAnnotation, on the soci-enabled image manifest itself,
	// links it to its ztoc artifact. Used as a fallback to identify the
	// runnable manifest.
	sociIndexDigestAnnotation = "com.amazon.soci.index-digest"
)

// ecrBatchGetImageAPI is the subset of the ECR client used here (for testing).
type ecrBatchGetImageAPI interface {
	BatchGetImage(ctx context.Context, params *ecr.BatchGetImageInput, optFns ...func(*ecr.Options)) (*ecr.BatchGetImageOutput, error)
}

// resolveAppStoreImageURL returns the image URL to hand back to an authorized
// caller. When a "<tag>-soci" SOCI v2 index exists for the resolved ECR image,
// it returns the digest of the soci-enabled image manifest so Fargate (platform
// version 1.4.0+) lazy-loads the image instead of full-pulling it — the AWS SOCI
// v2 index builder deliberately does NOT move the original tag, so the only way
// to engage lazy loading is to deploy the soci-enabled manifest digest.
//
// This handler runs in the platform account (same account as the appstore ECR
// repo and the SOCI index), so the lookup is same-account — no cross-account
// RegistryId/role needed.
//
// The change is surgical: it ONLY overrides the URL when a SOCI index is present
// (it's built asynchronously after the image push). For non-ECR images, digest
// pins, missing tags, no SOCI index, or any error, the original URL is returned
// unchanged.
func resolveAppStoreImageURL(ctx context.Context, ecrClient ecrBatchGetImageAPI, imageURL string) string {
	parts := strings.SplitN(imageURL, "/", 2)
	if len(parts) != 2 || !strings.Contains(parts[0], ".dkr.ecr.") {
		return imageURL // non-ECR registry or malformed — use as-is
	}
	repoAndTag := parts[1]
	if strings.Contains(repoAndTag, "@") {
		return imageURL // already a digest pin
	}
	idx := strings.LastIndex(repoAndTag, ":")
	if idx < 0 {
		return imageURL // no tag to look up
	}
	repoName, tag := repoAndTag[:idx], repoAndTag[idx+1:]

	out, err := ecrClient.BatchGetImage(ctx, &ecr.BatchGetImageInput{
		RepositoryName:     aws.String(repoName),
		ImageIds:           []ecrtypes.ImageIdentifier{{ImageTag: aws.String(tag + "-soci")}},
		AcceptedMediaTypes: []string{"application/vnd.oci.image.index.v1+json"},
	})
	if err != nil || len(out.Images) == 0 || out.Images[0].ImageManifest == nil {
		return imageURL // no SOCI index yet — leave the tag as-is
	}

	sociDigest := sociImageManifestDigest(*out.Images[0].ImageManifest)
	if sociDigest == "" {
		return imageURL
	}

	resolved := fmt.Sprintf("%s/%s@%s", parts[0], repoName, sociDigest)
	log.Printf("Resolved %s -> %s (SOCI-enabled, Fargate lazy load)", imageURL, resolved)
	return resolved
}

// sociImageManifestDigest parses a "<tag>-soci" OCI image index and returns the
// digest of the soci-enabled image manifest to deploy. Returns "" if it can't be
// determined (caller falls back to the original image URL).
func sociImageManifestDigest(manifestJSON string) string {
	var index struct {
		Manifests []struct {
			ArtifactType string            `json:"artifactType"`
			Digest       string            `json:"digest"`
			Annotations  map[string]string `json:"annotations"`
		} `json:"manifests"`
	}
	if err := json.Unmarshal([]byte(manifestJSON), &index); err != nil {
		return ""
	}
	// Preferred: the ztoc artifact names the soci-enabled image manifest digest.
	for _, m := range index.Manifests {
		if m.ArtifactType == sociIndexArtifactType {
			if d := m.Annotations[sociImageManifestDigestAnnotation]; d != "" {
				return d
			}
		}
	}
	// Fallback: the soci-enabled image manifest is the child (not the ztoc
	// artifact) carrying the index-digest link to its ztoc.
	for _, m := range index.Manifests {
		if m.ArtifactType == "" && m.Annotations[sociIndexDigestAnnotation] != "" {
			return m.Digest
		}
	}
	return ""
}

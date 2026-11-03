package lambdaeligibility

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ecr"
	ecrtypes "github.com/aws/aws-sdk-go-v2/service/ecr/types"
)

// ImageReader is the part of ECR ReadImage uses.
type ImageReader interface {
	BatchGetImage(ctx context.Context, in *ecr.BatchGetImageInput, opts ...func(*ecr.Options)) (*ecr.BatchGetImageOutput, error)
	GetDownloadUrlForLayer(ctx context.Context, in *ecr.GetDownloadUrlForLayerInput, opts ...func(*ecr.Options)) (*ecr.GetDownloadUrlForLayerOutput, error)
}

// HTTPDoer downloads an image's config blob from the link ECR gives.
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

var (
	indexMediaTypes = []string{"application/vnd.docker.distribution.manifest.list.v2+json", "application/vnd.oci.image.index.v1+json"}
	imageMediaTypes = []string{"application/vnd.docker.distribution.manifest.v2+json", "application/vnd.oci.image.manifest.v1+json"}
)

// ParseECRImage splits an ECR image (<account>.dkr.ecr.<region>.amazonaws.com/
// <repo>:<tag> or <repo>@<digest>) into its registry id, repository and
// identifier. ok is false for an image elsewhere.
func ParseECRImage(image string) (registry, repo string, id ecrtypes.ImageIdentifier, ok bool) {
	host, rest, found := strings.Cut(image, "/")
	if !found || !strings.Contains(host, ".dkr.ecr.") {
		return "", "", id, false
	}
	registry, _, _ = strings.Cut(host, ".")
	repo = rest
	if r, digest, isDigest := strings.Cut(rest, "@"); isDigest {
		repo, id.ImageDigest = r, aws.String(digest)
	} else if i := strings.LastIndex(rest, ":"); i >= 0 {
		repo, id.ImageTag = rest[:i], aws.String(rest[i+1:])
	} else {
		id.ImageTag = aws.String("latest")
	}
	return registry, repo, id, true
}

// ReadImage reads an image's facts from ECR: its manifest (for a
// multi-platform image, its linux/amd64 manifest, the platform Lambda runs
// an image on; without one, its first image's, which then isn't eligible),
// whose layers make its compressed size, then its config blob, with its
// platform and labels.
func ReadImage(ctx context.Context, client ImageReader, httpc HTTPDoer, image string) (ImageFacts, error) {
	var facts ImageFacts
	registry, repo, id, ok := ParseECRImage(image)
	if !ok {
		return facts, fmt.Errorf("%s is not an image in ECR", image)
	}
	manifest, mediaType, err := getManifest(ctx, client, registry, repo, id)
	if err != nil {
		return facts, err
	}
	if isIndex(mediaType) {
		digest, err := platformManifest(manifest)
		if err != nil {
			return facts, err
		}
		if manifest, _, err = getManifest(ctx, client, registry, repo, ecrtypes.ImageIdentifier{ImageDigest: aws.String(digest)}); err != nil {
			return facts, err
		}
	}
	var m struct {
		Config struct {
			Digest string `json:"digest"`
		} `json:"config"`
		Layers []struct {
			Size int64 `json:"size"`
		} `json:"layers"`
	}
	if err := json.Unmarshal(manifest, &m); err != nil || m.Config.Digest == "" {
		return facts, errors.New("the image manifest names no configuration")
	}
	for _, l := range m.Layers {
		facts.CompressedBytes += l.Size
	}
	link, err := client.GetDownloadUrlForLayer(ctx, &ecr.GetDownloadUrlForLayerInput{
		RegistryId: aws.String(registry), RepositoryName: aws.String(repo), LayerDigest: aws.String(m.Config.Digest),
	})
	if err != nil {
		return facts, fmt.Errorf("getting the image configuration's link: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, aws.ToString(link.DownloadUrl), nil)
	if err != nil {
		return facts, err
	}
	resp, err := httpc.Do(req)
	if err != nil {
		// the error would carry the presigned link
		return facts, errors.New("downloading the image configuration failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return facts, fmt.Errorf("downloading the image configuration: HTTP %d", resp.StatusCode)
	}
	var blob struct {
		OS           string `json:"os"`
		Architecture string `json:"architecture"`
		Config       struct {
			Labels map[string]string `json:"Labels"`
		} `json:"config"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&blob); err != nil {
		return facts, fmt.Errorf("decoding the image configuration: %w", err)
	}
	facts.OS, facts.Architecture, facts.Labels = blob.OS, blob.Architecture, blob.Config.Labels
	return facts, nil
}

// platformManifest is the digest of an image index's linux/amd64 image, or
// of its first image (not an attestation) when it has none.
func platformManifest(index []byte) (string, error) {
	var idx struct {
		Manifests []struct {
			Digest   string `json:"digest"`
			Platform struct {
				Architecture string `json:"architecture"`
				OS           string `json:"os"`
			} `json:"platform"`
		} `json:"manifests"`
	}
	if err := json.Unmarshal(index, &idx); err != nil {
		return "", fmt.Errorf("decoding the image index: %w", err)
	}
	first := ""
	for _, m := range idx.Manifests {
		if m.Platform.OS == "linux" && m.Platform.Architecture == "amd64" {
			return m.Digest, nil
		}
		if first == "" && m.Platform.OS != "unknown" {
			first = m.Digest
		}
	}
	if first == "" {
		return "", errors.New("the image index lists no image")
	}
	return first, nil
}

func getManifest(ctx context.Context, client ImageReader, registry, repo string, id ecrtypes.ImageIdentifier) ([]byte, string, error) {
	out, err := client.BatchGetImage(ctx, &ecr.BatchGetImageInput{
		RegistryId:         aws.String(registry),
		RepositoryName:     aws.String(repo),
		ImageIds:           []ecrtypes.ImageIdentifier{id},
		AcceptedMediaTypes: append(append([]string{}, imageMediaTypes...), indexMediaTypes...),
	})
	if err != nil {
		return nil, "", fmt.Errorf("reading the image manifest: %w", err)
	}
	if len(out.Images) == 0 {
		return nil, "", errors.New("the image is not in ECR")
	}
	mediaType := aws.ToString(out.Images[0].ImageManifestMediaType)
	manifest := []byte(aws.ToString(out.Images[0].ImageManifest))
	if mediaType == "" {
		// ECR leaves it out for some images; the manifest says.
		var m struct {
			MediaType string `json:"mediaType"`
		}
		_ = json.Unmarshal(manifest, &m)
		mediaType = m.MediaType
	}
	return manifest, mediaType, nil
}

func isIndex(mediaType string) bool {
	for _, t := range indexMediaTypes {
		if t == mediaType {
			return true
		}
	}
	return false
}

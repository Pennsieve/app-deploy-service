package handler

import (
	"context"
	"log"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	ghsync "github.com/pennsieve/github-client/pkg/github/sync"
	"gopkg.in/yaml.v3"
)

const (
	// appConfigFile is the file in the repo that declares runtime requirements.
	appConfigFile = "app.yml"
	// gpuBuildStorageGiB is the ephemeral storage (GiB) allocated to the build
	// task when an app declares a "gpu" compute type. GPU apps pull large
	// dependencies (e.g. torch+cuda) that exhaust Fargate's default 20 GiB.
	gpuBuildStorageGiB int32 = 100
)

// appConfig is a partial view of app.yml, limited to the fields that affect how
// the image is built. app.yml may be authored as YAML or JSON; yaml.v3 parses
// both.
type appConfig struct {
	Runtime struct {
		ComputeTypes []string `yaml:"computeTypes"`
	} `yaml:"runtime"`
}

func (c appConfig) needsGPU() bool {
	for _, t := range c.Runtime.ComputeTypes {
		if strings.EqualFold(strings.TrimSpace(t), "gpu") {
			return true
		}
	}
	return false
}

func parseAppConfig(data []byte) (appConfig, error) {
	var c appConfig
	if err := yaml.Unmarshal(data, &c); err != nil {
		return appConfig{}, err
	}
	return c, nil
}

// buildStorageGiB returns the ephemeral storage override (GiB) for the build,
// or 0 when no override is needed (use the Fargate default).
func buildStorageGiB(c appConfig) int32 {
	if c.needsGPU() {
		return gpuBuildStorageGiB
	}
	return 0
}

// detectBuildStorageGiB reads the synced app.yml from S3 and derives the
// ephemeral storage the build task needs. It returns 0 (no override) on any
// failure so a missing or malformed app.yml never blocks a deployment.
func detectBuildStorageGiB(ctx context.Context, cfg aws.Config, sourceUrl string, tag string) int32 {
	bucket := os.Getenv("CONTENT_SYNC_BUCKET")
	if bucket == "" {
		return 0
	}
	if tag == "" {
		tag = "main"
	}

	namespace := buildNamespace(sourceUrl, tag)
	s3Client := s3.NewFromConfig(cfg)
	dest := ghsync.NewS3Destination(s3Client, bucket)

	key := namespace + "/" + appConfigFile
	data, _, err := dest.Read(ctx, key)
	if err != nil {
		log.Printf("warning: unable to read %s for build sizing: %v", key, err)
		return 0
	}

	c, err := parseAppConfig(data)
	if err != nil {
		log.Printf("warning: unable to parse %s for build sizing: %v", key, err)
		return 0
	}

	return buildStorageGiB(c)
}

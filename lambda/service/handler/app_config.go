package handler

import (
	"context"
	"log"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/pennsieve/app-deploy-service/service/store_dynamodb"
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

// appConfig is a partial view of app.yml, limited to the fields consumed by
// this service: build-affecting runtime hints and the parameter declarations
// (with defaults) surfaced to downstream services. app.yml is authored as YAML.
type appConfig struct {
	Runtime struct {
		ComputeTypes []string `yaml:"computeTypes"`
	} `yaml:"runtime"`
	Parameters []appParameter `yaml:"parameters"`
}

// appParameter mirrors a single entry of app.yml's `parameters` list. A
// parameter with no DefaultValue is treated as required by consumers.
type appParameter struct {
	Name         string   `yaml:"name"`
	Type         string   `yaml:"type"`
	Description  string   `yaml:"description"`
	DefaultValue string   `yaml:"defaultValue"`
	ValidValues  []string `yaml:"validValues"`
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

// readSyncedAppConfig reads the synced app.yml from S3 and parses it. The bool
// is false (and appConfig zero) on any failure — missing bucket, missing file,
// or malformed YAML — so a missing or malformed app.yml never blocks a
// deployment nor overwrites previously stored values.
func readSyncedAppConfig(ctx context.Context, cfg aws.Config, sourceUrl string, tag string) (appConfig, bool) {
	bucket := os.Getenv("CONTENT_SYNC_BUCKET")
	if bucket == "" {
		return appConfig{}, false
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
		log.Printf("warning: unable to read %s: %v", key, err)
		return appConfig{}, false
	}

	c, err := parseAppConfig(data)
	if err != nil {
		log.Printf("warning: unable to parse %s: %v", key, err)
		return appConfig{}, false
	}

	return c, true
}

// appParameters maps the parsed app.yml parameter declarations onto the stored
// representation surfaced to downstream services (e.g. workflow-service reads
// these as processor parameter defaults). Entries without a name are skipped.
func appParameters(c appConfig) []store_dynamodb.AppParameter {
	if len(c.Parameters) == 0 {
		return nil
	}
	params := make([]store_dynamodb.AppParameter, 0, len(c.Parameters))
	for _, p := range c.Parameters {
		if p.Name == "" {
			continue
		}
		params = append(params, store_dynamodb.AppParameter{
			Name:         p.Name,
			Type:         p.Type,
			Description:  p.Description,
			DefaultValue: p.DefaultValue,
			ValidValues:  p.ValidValues,
		})
	}
	return params
}

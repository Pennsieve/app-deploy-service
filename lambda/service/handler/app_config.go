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

// appConfig is app.yml as this service reads it: what the app is, its
// runtime (build sizing and what it asks for), its parameters and its
// inputs and outputs. app.yml is authored as YAML; fields it doesn't declare
// stay zero.
type appConfig struct {
	Application appInfo `yaml:"application"`
	Runtime     struct {
		CPU            int      `yaml:"cpu"`
		Memory         int      `yaml:"memory"`
		ComputeTypes   []string `yaml:"computeTypes"`
		TimeoutSeconds int      `yaml:"timeoutSeconds"`
		// Interactive: a human-in-the-loop app, run as a live session of
		// SessionType ("jupyter" is a notebook); Kernel is a Jupyter
		// session's language ("python", "r").
		Interactive bool   `yaml:"interactive"`
		SessionType string `yaml:"sessionType"`
		Kernel      string `yaml:"kernel"`
	} `yaml:"runtime"`
	Parameters []appParameter `yaml:"parameters"`
	Inputs     []appPort      `yaml:"inputs"`
	Outputs    []appPort      `yaml:"outputs"`
}

// appInfo is what the app is, under `application:`.
type appInfo struct {
	Name        string          `yaml:"name"`
	Description string          `yaml:"description"`
	Tags        []string        `yaml:"tags"`
	Maintainers []appMaintainer `yaml:"maintainers"`
}

// appMaintainer is one of `application.maintainers`.
type appMaintainer struct {
	Name  string `yaml:"name"`
	Email string `yaml:"email"`
	Orcid string `yaml:"orcid"`
}

// appPort mirrors an entry of app.yml's `inputs` or `outputs`: MIME types,
// EDAM format ids (format_3590, or its http://edamontology.org/ URI), or
// neither, meaning any file. Container says whether folder-format items are
// given as folders or as stored; ConformsTo names the folder structure(s) the
// whole port follows (bids@1.9), outermost first.
type appPort struct {
	Name        string     `yaml:"name"`
	Description string     `yaml:"description"`
	MediaTypes  []string   `yaml:"mediaTypes"`
	Formats     []string   `yaml:"formats"`
	Container   string     `yaml:"container"`
	ConformsTo  stringList `yaml:"conformsTo"`
	Placement   string     `yaml:"placement"`
}

// stringList reads one string or a list of them (conformsTo: bids, or
// conformsTo: [pennsieve-published-dataset, bids]).
type stringList []string

func (l *stringList) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind == yaml.ScalarNode {
		*l = stringList{value.Value}
		return nil
	}
	var list []string
	if err := value.Decode(&list); err != nil {
		return err
	}
	*l = list
	return nil
}

// trimmed drops blank entries and surrounding space; nil when nothing is left.
func (l stringList) trimmed() []string {
	var out []string
	for _, s := range l {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// appParameter mirrors a single entry of app.yml's `parameters` list.
// Required defaults to false when omitted.
type appParameter struct {
	Name         string   `yaml:"name"`
	Type         string   `yaml:"type"`
	Required     bool     `yaml:"required"`
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
	data, ok := readSyncedAppYml(ctx, cfg, sourceUrl, tag)
	if !ok {
		return appConfig{}, false
	}
	c, err := parseAppConfig(data)
	if err != nil {
		log.Printf("warning: unable to parse the app.yml of %s %s: %v", sourceUrl, tag, err)
		return appConfig{}, false
	}
	return c, true
}

// readSyncedAppYml reads the synced app.yml's text from S3; false when there's
// none (or it can't be read).
func readSyncedAppYml(ctx context.Context, cfg aws.Config, sourceUrl string, tag string) ([]byte, bool) {
	bucket := os.Getenv("CONTENT_SYNC_BUCKET")
	if bucket == "" {
		return nil, false
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
		return nil, false
	}
	return data, true
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
			Required:     p.Required,
			Description:  p.Description,
			DefaultValue: p.DefaultValue,
			ValidValues:  p.ValidValues,
		})
	}
	return params
}

// appManifest is the version's record of its app.yml: what the app is, what
// it asks for, its parameters, and what goes in and out. Stored per version,
// so a workflow pinned to a version can be shown and run with that version's.
func appManifest(c appConfig) *store_dynamodb.AppManifest {
	return &store_dynamodb.AppManifest{
		Name:        c.Application.Name,
		Description: c.Application.Description,
		Tags:        c.Application.Tags,
		Maintainers: appMaintainers(c.Application.Maintainers),
		Runtime: store_dynamodb.AppRuntime{
			CPU:            c.Runtime.CPU,
			Memory:         c.Runtime.Memory,
			ComputeTypes:   c.Runtime.ComputeTypes,
			TimeoutSeconds: c.Runtime.TimeoutSeconds,
			Interactive:    c.Runtime.Interactive || c.Runtime.SessionType != "",
			SessionType:    c.Runtime.SessionType,
			Kernel:         c.Runtime.Kernel,
		},
		Parameters: appParameters(c),
		Inputs:     appPorts(c.Inputs),
		Outputs:    appPorts(c.Outputs),
	}
}

func appPorts(ports []appPort) []store_dynamodb.AppPort {
	if len(ports) == 0 {
		return nil
	}
	out := make([]store_dynamodb.AppPort, 0, len(ports))
	for _, p := range ports {
		if p.Name == "" {
			continue
		}
		out = append(out, store_dynamodb.AppPort{Name: p.Name, Description: p.Description, MediaTypes: p.MediaTypes, Formats: edamFormats(p.Formats),
			Container: strings.TrimSpace(p.Container), ConformsTo: p.ConformsTo.trimmed(),
			Placement: strings.TrimSpace(p.Placement)})
	}
	return out
}

// appMaintainers keeps the named maintainers, each ORCID as its bare iD.
func appMaintainers(ms []appMaintainer) []store_dynamodb.AppMaintainer {
	var out []store_dynamodb.AppMaintainer
	for _, m := range ms {
		if strings.TrimSpace(m.Name) == "" {
			continue
		}
		out = append(out, store_dynamodb.AppMaintainer{
			Name:  strings.TrimSpace(m.Name),
			Email: strings.TrimSpace(m.Email),
			Orcid: strings.TrimPrefix(strings.TrimSpace(m.Orcid), "https://orcid.org/"),
		})
	}
	return out
}

// edamFormats keeps EDAM format ids short (format_3590), whether written so
// or as their http://edamontology.org/ URI.
func edamFormats(fs []string) []string {
	if len(fs) == 0 {
		return nil
	}
	out := make([]string, 0, len(fs))
	for _, f := range fs {
		if f = strings.TrimPrefix(strings.TrimSpace(f), "http://edamontology.org/"); f != "" {
			out = append(out, f)
		}
	}
	return out
}

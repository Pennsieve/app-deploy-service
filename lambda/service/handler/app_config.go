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
		// LLM: the app calls language models through Pennsieve, so its runs
		// get the LLM proxy sidecar (opt-in).
		LLM bool `yaml:"llm"`
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
	// Build is how the image is built (build args, target stage,
	// Dockerfile); nil when app.yml doesn't say.
	Build *appBuild `yaml:"build"`
	// Variants are flavours of the app built from the same release, each the
	// top level with its own overrides (resolveVariants); none for most apps.
	Variants []appVariant `yaml:"variants"`
	// Review is how a release is tested before it's made public; nil when
	// app.yml doesn't say.
	Review *appReview `yaml:"review"`
}

// appReview is app.yml's `review`: a command that generates the whole input
// directory, per input port where a review run gets its data instead, and
// review-only parameter values (as written). (Which outputs a run must fill
// is the outputs' own `required`.)
// It's read leniently (UnmarshalYAML) so a malformed block, which the schema
// reports, doesn't fail reading the rest of app.yml: what doesn't parse is
// left out.
// The yaml tags aren't used for decoding; they name the fields for the
// schema drift test (TestParserFieldsAreInTheSchema).
type appReview struct {
	Version    int                       `yaml:"version"`
	Generate   *appReviewGenerate        `yaml:"generate"`
	Inputs     map[string]appReviewInput `yaml:"inputs"`
	Parameters map[string]string         `yaml:"parameters"`
	// otherParameters are the names of parameters whose value isn't a
	// scalar (the schema reports them), so reviewProblems still checks
	// the name.
	otherParameters []string
}

// appReviewGenerate is a review `generate`, at the top of the block or for
// one port.
type appReviewGenerate struct {
	Image   string            `yaml:"image"`
	Command string            `yaml:"command"`
	Root    string            `yaml:"root"`
	Source  *appReviewDataset `yaml:"source"`
}

// appReviewInput is one of review.inputs: generate, files or dataset.
type appReviewInput struct {
	Generate *appReviewGenerate `yaml:"generate"`
	Files    []string           `yaml:"files"`
	Dataset  *appReviewDataset  `yaml:"dataset"`
}

// appReviewDataset is a public dataset, by doi or url.
type appReviewDataset struct {
	DOI   string   `yaml:"doi"`
	URL   string   `yaml:"url"`
	Files []string `yaml:"files"`
}

// UnmarshalYAML reads each part of the block on its own and never fails:
// a part that doesn't parse is skipped (an input port is kept, empty, so
// its name is still checked).
func (r *appReview) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(value.Content); i += 2 {
		v := value.Content[i+1]
		switch value.Content[i].Value {
		case "version":
			_ = v.Decode(&r.Version)
		case "generate":
			var g appReviewGenerate
			if v.Decode(&g) == nil {
				r.Generate = &g
			}
		case "inputs":
			if v.Kind != yaml.MappingNode {
				continue
			}
			r.Inputs = map[string]appReviewInput{}
			for j := 0; j+1 < len(v.Content); j += 2 {
				var in appReviewInput
				if v.Content[j+1].Decode(&in) != nil {
					in = appReviewInput{}
				}
				r.Inputs[v.Content[j].Value] = in
			}
		case "parameters":
			if v.Kind != yaml.MappingNode {
				continue
			}
			r.Parameters = map[string]string{}
			for j := 0; j+1 < len(v.Content); j += 2 {
				name, p := v.Content[j].Value, v.Content[j+1]
				if p.Kind == yaml.ScalarNode {
					r.Parameters[name] = p.Value
				} else {
					r.otherParameters = append(r.otherParameters, name)
				}
			}
		}
	}
	return nil
}

// appBuild is app.yml's `build`, at the top level or on a variant.
type appBuild struct {
	Args       map[string]string `yaml:"args"`
	Target     string            `yaml:"target"`
	Dockerfile string            `yaml:"dockerfile"`
}

// appVariant is one of app.yml's `variants`: what it changes about the top
// level. Pointers and nil slices mean "not overridden".
type appVariant struct {
	ID          string                          `yaml:"id"`
	Name        string                          `yaml:"name"`
	Description string                          `yaml:"description"`
	Default     bool                            `yaml:"default"`
	Build       *appBuild                       `yaml:"build"`
	Runtime     appVariantRuntime               `yaml:"runtime"`
	Parameters  map[string]appParameterOverride `yaml:"parameters"`
}

// appVariantRuntime is the runtime fields a variant may replace.
// interactive, sessionType and kernel are the top level's only.
type appVariantRuntime struct {
	CPU            *int     `yaml:"cpu"`
	Memory         *int     `yaml:"memory"`
	ComputeTypes   []string `yaml:"computeTypes"`
	TimeoutSeconds *int     `yaml:"timeoutSeconds"`
	LLM            *bool    `yaml:"llm"`
}

// appParameterOverride is what a variant changes about one top-level
// parameter, keyed by its name.
type appParameterOverride struct {
	Label        *string  `yaml:"label"`
	Description  *string  `yaml:"description"`
	Required     *bool    `yaml:"required"`
	DefaultValue *string  `yaml:"defaultValue"`
	ValidValues  []string `yaml:"validValues"`
}

// appInfo is what the app is, under `application:`.
type appInfo struct {
	Name        string          `yaml:"name"`
	Description string          `yaml:"description"`
	Tags        []string        `yaml:"tags"`
	Maintainers []appMaintainer `yaml:"maintainers"`
	// Relations types works listed in the repository's CITATION.cff that
	// aren't plain references: DOI → DataCite relation type.
	Relations map[string]string `yaml:"relations"`
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
// Required defaults to false when omitted. Label, Min and Max are read to
// check variants' overrides against; they aren't stored.
type appParameter struct {
	Name         string   `yaml:"name"`
	Type         string   `yaml:"type"`
	Label        string   `yaml:"label"`
	Required     bool     `yaml:"required"`
	Description  string   `yaml:"description"`
	DefaultValue string   `yaml:"defaultValue"`
	ValidValues  []string `yaml:"validValues"`
	Min          *float64 `yaml:"min"`
	Max          *float64 `yaml:"max"`
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
	return readSyncedFile(ctx, cfg, sourceUrl, tag, appConfigFile)
}

// readSyncedFile reads one of a tag's synced files (CONTENT_SYNC_FILES) from
// S3; false when there's none (or it can't be read).
func readSyncedFile(ctx context.Context, cfg aws.Config, sourceUrl string, tag string, file string) ([]byte, bool) {
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

	key := namespace + "/" + file
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
			LLM:            c.Runtime.LLM,
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

// appReviewRecord is the version's record of app.yml's review block; nil
// when there's none, or nothing of it could be read. An input port whose
// source didn't parse is left out.
func appReviewRecord(r *appReview) *store_dynamodb.AppReview {
	if r == nil {
		return nil
	}
	out := &store_dynamodb.AppReview{
		Version:  r.Version,
		Generate: reviewGenerateRecord(r.Generate),
	}
	for name, in := range r.Inputs {
		rec := store_dynamodb.ReviewInput{
			Generate: reviewGenerateRecord(in.Generate),
			Files:    in.Files,
			Dataset:  reviewDatasetRecord(in.Dataset),
		}
		if rec.Generate == nil && len(rec.Files) == 0 && rec.Dataset == nil {
			continue
		}
		if out.Inputs == nil {
			out.Inputs = map[string]store_dynamodb.ReviewInput{}
		}
		out.Inputs[name] = rec
	}
	if len(r.Parameters) > 0 {
		out.Parameters = r.Parameters
	}
	if out.Version == 0 && out.Generate == nil && out.Inputs == nil && out.Parameters == nil {
		return nil
	}
	return out
}

func reviewGenerateRecord(g *appReviewGenerate) *store_dynamodb.ReviewGenerate {
	if g == nil {
		return nil
	}
	return &store_dynamodb.ReviewGenerate{Image: g.Image, Command: g.Command, Root: g.Root, Source: reviewDatasetRecord(g.Source)}
}

func reviewDatasetRecord(d *appReviewDataset) *store_dynamodb.ReviewDataset {
	if d == nil {
		return nil
	}
	return &store_dynamodb.ReviewDataset{DOI: d.DOI, URL: d.URL, Files: d.Files}
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

package handler

import (
	"regexp"
	"sort"
	"strings"

	"github.com/pennsieve/app-deploy-service/service/store_dynamodb"
)

// maxVariants caps app.yml's variants: each is its own build on every release.
const maxVariants = 8

// variantIDPattern is a variant id: lowercase letters, numbers and single
// dashes inside, at most 32 characters. It is part of the image tag
// (<hash>-<tag>--<id>), so "--" can't appear in it, and "default" is
// reserved.
var variantIDPattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,30}[a-z0-9])?$`)

func validVariantID(id string) bool {
	return variantIDPattern.MatchString(id) && id != "default" && !strings.Contains(id, "--")
}

// resolvedVariant is one variant of an app.yml: the top level with the
// variant's overrides applied (Config), and its merged build settings.
type resolvedVariant struct {
	ID          string
	Name        string
	Description string
	Config      appConfig
	Build       appBuild
}

// resolvedApp is an app.yml with its variants resolved. Default is what
// readers that don't know about variants see: the default variant's
// resolved config, or the app.yml as it is when it has no variants.
type resolvedApp struct {
	Default        appConfig
	DefaultVariant string
	Variants       []resolvedVariant // in app.yml order; none without variants
	// Build is the top-level build settings; kept on the version only when
	// there are no variants (each variant carries its merged copy).
	Build *appBuild
}

// resolveVariants resolves each of app.yml's variants to its effective
// config: the top level, with the variant's runtime fields replaced (not
// deep-merged), its parameter overrides applied to the named top-level
// parameters, and its build merged over the top-level build (args by
// name; target and dockerfile replaced). application, inputs, outputs,
// tags and interactive/session settings are always the top level's.
//
// The default is the variant with default: true, otherwise the first.
// Variants that can't be told apart safely (an invalid, reserved or
// repeated id) are left out, and only the first maxVariants are kept, so a
// manifest registered with problems in warn mode still resolves; its
// problems are reported by manifestProblems.
func resolveVariants(c appConfig) resolvedApp {
	top := c
	top.Variants = nil
	top.Build = nil
	r := resolvedApp{Default: top, Build: c.Build}

	seen := map[string]bool{}
	defaultIdx := -1
	for _, v := range c.Variants {
		if !validVariantID(v.ID) || seen[v.ID] {
			continue
		}
		if len(r.Variants) == maxVariants {
			break
		}
		seen[v.ID] = true
		if v.Default && defaultIdx < 0 {
			defaultIdx = len(r.Variants)
		}
		r.Variants = append(r.Variants, resolveVariant(top, c.Build, v))
	}
	if len(r.Variants) == 0 {
		return r
	}
	if defaultIdx < 0 {
		defaultIdx = 0
	}
	r.Default = r.Variants[defaultIdx].Config
	r.DefaultVariant = r.Variants[defaultIdx].ID
	return r
}

func resolveVariant(top appConfig, topBuild *appBuild, v appVariant) resolvedVariant {
	cfg := top
	if v.Runtime.CPU != nil {
		cfg.Runtime.CPU = *v.Runtime.CPU
	}
	if v.Runtime.Memory != nil {
		cfg.Runtime.Memory = *v.Runtime.Memory
	}
	if v.Runtime.ComputeTypes != nil {
		cfg.Runtime.ComputeTypes = append([]string(nil), v.Runtime.ComputeTypes...)
	}
	if v.Runtime.TimeoutSeconds != nil {
		cfg.Runtime.TimeoutSeconds = *v.Runtime.TimeoutSeconds
	}
	if v.Runtime.LLM != nil {
		cfg.Runtime.LLM = *v.Runtime.LLM
	}
	if len(top.Parameters) > 0 {
		cfg.Parameters = make([]appParameter, len(top.Parameters))
		for i, p := range top.Parameters {
			if o, ok := v.Parameters[p.Name]; ok && p.Name != "" {
				p = p.withOverride(o)
			}
			cfg.Parameters[i] = p
		}
	}

	name := strings.TrimSpace(v.Name)
	if name == "" {
		name = v.ID
	}
	return resolvedVariant{
		ID:          v.ID,
		Name:        name,
		Description: strings.TrimSpace(v.Description),
		Config:      cfg,
		Build:       mergeBuild(topBuild, v.Build),
	}
}

// withOverride is the parameter with a variant's overrides applied.
func (p appParameter) withOverride(o appParameterOverride) appParameter {
	if o.Label != nil {
		p.Label = *o.Label
	}
	if o.Description != nil {
		p.Description = *o.Description
	}
	if o.Required != nil {
		p.Required = *o.Required
	}
	if o.DefaultValue != nil {
		p.DefaultValue = *o.DefaultValue
	}
	if o.ValidValues != nil {
		p.ValidValues = append([]string(nil), o.ValidValues...)
	} else if p.ValidValues != nil {
		p.ValidValues = append([]string(nil), p.ValidValues...)
	}
	return p
}

// mergeBuild is a variant's build settings: its args over the top-level
// args, and its target and dockerfile in place of the top level's.
func mergeBuild(top, v *appBuild) appBuild {
	var b appBuild
	if top != nil {
		b.Target, b.Dockerfile = top.Target, top.Dockerfile
		for k, val := range top.Args {
			if b.Args == nil {
				b.Args = map[string]string{}
			}
			b.Args[k] = val
		}
	}
	if v != nil {
		if v.Target != "" {
			b.Target = v.Target
		}
		if v.Dockerfile != "" {
			b.Dockerfile = v.Dockerfile
		}
		for k, val := range v.Args {
			if b.Args == nil {
				b.Args = map[string]string{}
			}
			b.Args[k] = val
		}
	}
	return b
}

// manifest is the variant's resolved manifest, naming the variant.
func (v resolvedVariant) manifest() *store_dynamodb.AppManifest {
	m := appManifest(v.Config)
	m.Variant = &store_dynamodb.AppManifestVariant{ID: v.ID, Name: v.Name, Description: v.Description}
	return m
}

// versionManifest is what a version stores as its manifest: the default
// variant's resolved manifest, or, without variants, exactly what
// appManifest makes of the app.yml.
func (r resolvedApp) versionManifest() *store_dynamodb.AppManifest {
	for _, v := range r.Variants {
		if v.ID == r.DefaultVariant {
			return v.manifest()
		}
	}
	return appManifest(r.Default)
}

// parameters are the app's top-level params: the default variant's.
func (r resolvedApp) parameters() []store_dynamodb.AppParameter {
	return appParameters(r.Default)
}

// versionVariants is what a version records besides its manifest: its
// variants, or, without variants, the top-level build settings. Empty for
// an app.yml with neither, which leaves the version record as it always was.
func (r resolvedApp) versionVariants() store_dynamodb.VersionVariants {
	if len(r.Variants) == 0 {
		return store_dynamodb.VersionVariants{Build: storedBuild(r.Build)}
	}
	vv := store_dynamodb.VersionVariants{
		DefaultVariant: r.DefaultVariant,
		VariantOrder:   make([]string, 0, len(r.Variants)),
		Variants:       make(map[string]store_dynamodb.AppVariant, len(r.Variants)),
	}
	for _, v := range r.Variants {
		b := v.Build
		vv.VariantOrder = append(vv.VariantOrder, v.ID)
		vv.Variants[v.ID] = store_dynamodb.AppVariant{
			Name:        v.Name,
			Description: v.Description,
			Manifest:    v.manifest(),
			Build:       storedBuild(&b),
		}
	}
	return vv
}

// storedBuild is the build settings as stored; nil when they say nothing.
func storedBuild(b *appBuild) *store_dynamodb.AppBuild {
	if b == nil {
		return nil
	}
	out := &store_dynamodb.AppBuild{Target: strings.TrimSpace(b.Target), Dockerfile: strings.TrimSpace(b.Dockerfile)}
	if len(b.Args) > 0 {
		out.Args = make(map[string]string, len(b.Args))
		for k, v := range b.Args {
			out.Args[k] = v
		}
	}
	if out.Args == nil && out.Target == "" && out.Dockerfile == "" {
		return nil
	}
	return out
}

// sortedKeys returns a map's keys in order, for stable problem reports.
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

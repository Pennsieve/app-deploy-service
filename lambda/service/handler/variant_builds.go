package handler

import (
	"os"
	"path"
	"strings"

	"github.com/pennsieve/app-deploy-service/service/store_dynamodb"
)

// appVariantsEnabledEnv turns building every variant on a release on or off
// (APP_VARIANTS_ENABLED). Anything but "false" builds every variant; "false"
// builds only the default variant, as before variant builds existed. Either
// way the default variant is built with its own build settings.
const appVariantsEnabledEnv = "APP_VARIANTS_ENABLED"

// readVersionBuildsEnv tells the provisioner task to read the version's
// build settings and variants from its record (READ_VERSION_BUILDS=true).
// Not set for an app.yml with neither, so those releases take exactly the
// path they always took.
const readVersionBuildsEnv = "READ_VERSION_BUILDS"

// registrationRegistering is a variant whose build has not started yet.
const registrationRegistering = "registering"

func variantBuildsEnabled() bool {
	return strings.ToLower(strings.TrimSpace(os.Getenv(appVariantsEnabledEnv))) != "false"
}

// variantDeployment is the deployment record of one variant's build.
type variantDeployment struct {
	Variant      string
	DeploymentId string
}

// planVariantBuilds decides which of a version's variants are built, and
// records it on the variants before they are stored: each built variant
// gets its deployment id, the disk its build needs and, for a non-default
// variant, the status "registering". The default variant is built by the
// release's own deployment (defaultDeploymentId); its status is the
// version's until its build ends, so none is stored for it here. With
// buildAll false only the default variant is built, and the others keep no
// deployment and no status (the registry calls them not yet deployed).
//
// It returns every built variant's deployment, the default's first. Nothing
// for a version without variants.
func planVariantBuilds(vv *store_dynamodb.VersionVariants, r resolvedApp, defaultDeploymentId string, buildAll bool, newId func() string) []variantDeployment {
	if len(r.Variants) == 0 || len(vv.Variants) == 0 {
		return nil
	}
	var out []variantDeployment
	for _, v := range r.Variants {
		sv, ok := vv.Variants[v.ID]
		if !ok {
			continue
		}
		isDefault := v.ID == r.DefaultVariant
		if !isDefault && !buildAll {
			continue
		}
		sv.BuildStorageGiB = buildStorageGiB(v.Config)
		if isDefault {
			sv.DeploymentId = defaultDeploymentId
		} else {
			sv.DeploymentId = newId()
			sv.Status = registrationRegistering
		}
		vv.Variants[v.ID] = sv
		dep := variantDeployment{Variant: v.ID, DeploymentId: sv.DeploymentId}
		if isDefault {
			out = append([]variantDeployment{dep}, out...)
		} else {
			out = append(out, dep)
		}
	}
	return out
}

// builtDockerfiles are the distinct Dockerfiles a release builds, as paths
// in the repository: "Dockerfile" unless app.yml's build (or a built
// variant's) names another. The kaniko guard reads each one.
func builtDockerfiles(r resolvedApp, buildAll bool) []string {
	var files []string
	seen := map[string]bool{}
	add := func(p string) {
		p = strings.TrimSpace(p)
		if p == "" {
			p = "Dockerfile"
		}
		p = strings.TrimPrefix(path.Clean(p), "/")
		if !seen[p] {
			seen[p] = true
			files = append(files, p)
		}
	}
	if len(r.Variants) == 0 {
		if r.Build != nil {
			add(r.Build.Dockerfile)
		} else {
			add("")
		}
		return files
	}
	for _, v := range r.Variants {
		if v.ID == r.DefaultVariant || buildAll {
			add(v.Build.Dockerfile)
		}
	}
	return files
}

// defaultVariantBuilt is the variant the release's own deployment builds:
// the default variant's id when the version's variants are built, "" for
// an app without variants (its deployment record is as it always was).
func (r resolvedApp) defaultVariantBuilt(planned []variantDeployment) string {
	if len(planned) == 0 {
		return ""
	}
	return r.DefaultVariant
}

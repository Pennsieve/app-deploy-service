// Package imagebuild starts App Store image builds on AWS CodeBuild with
// BuildKit, the replacement for the kaniko Fargate task (docs/builds.md).
//
// The inputs and output are the kaniko task's: the app's git source at a tag
// is built and pushed to <appstore-private>:<hash>-<tag>. The build runs
// terraform/image-builder/build.sh in the environment's CodeBuild project;
// the status lambda follows it through CodeBuild's "Build State Change"
// events.
package imagebuild

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/codebuild"
	cbtypes "github.com/aws/aws-sdk-go-v2/service/codebuild/types"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	ssmtypes "github.com/aws/aws-sdk-go-v2/service/ssm/types"
)

// Builders, as the image_builder Terraform variable names them.
const (
	BuilderKaniko    = "kaniko"
	BuilderCodeBuild = "codebuild"
)

// Environment variables the provisioner task reads (task_definition.json.tpl).
const (
	BuilderEnv          = "IMAGE_BUILDER"
	ProjectEnv          = "IMAGE_BUILDER_PROJECT"
	CacheRepoEnv        = "BUILD_CACHE_ECR_URL"
	GitTokenPrefixEnv   = "IMAGE_BUILD_PARAMETER_PREFIX"
	defaultCacheKeySize = 40 // hex characters of the sha256 of the source URL
)

// Environment variables a build gets (build.sh and the status lambda read them).
const (
	ContextVar           = "CONTEXT"
	DestinationVar       = "DESTINATION"
	CacheRefVar          = "BUILD_CACHE_REF"
	GitTokenVar          = "GIT_TOKEN"
	DeploymentIdVar      = "DEPLOYMENT_ID"
	ApplicationIdVar     = "APPLICATION_ID"
	ApplicationsTableVar = "APPLICATIONS_TABLE"
)

// largeDiskGiB is the disk of BUILD_GENERAL1_LARGE, the project's default
// compute type. A build that needs more storage (APP_STORAGE) runs on
// BUILD_GENERAL1_XLARGE (256 GB) instead.
const largeDiskGiB = 128

// Selected returns the configured builder: kaniko unless IMAGE_BUILDER says
// codebuild. An unknown value is an error, so a typo in Terraform fails the
// release instead of silently picking a builder.
func Selected(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", BuilderKaniko:
		return BuilderKaniko, nil
	case BuilderCodeBuild:
		return BuilderCodeBuild, nil
	default:
		return "", fmt.Errorf("unknown %s %q (want %q or %q)", BuilderEnv, value, BuilderKaniko, BuilderCodeBuild)
	}
}

// Config is the environment's CodeBuild setup (from Terraform).
type Config struct {
	// ProjectName is the CodeBuild project that runs build.sh.
	ProjectName string
	// CacheRepoUrl is the build-cache ECR repository, kept apart from
	// appstore-private so cache pushes never reach the SOCI index builder.
	CacheRepoUrl string
	// GitTokenParameterPrefix is the SSM path a private repo's token is
	// put under for the build to read (never a plaintext build variable).
	GitTokenParameterPrefix string
}

// ConfigFromEnv reads Config from the provisioner's environment.
func ConfigFromEnv(getenv func(string) string) (Config, error) {
	c := Config{
		ProjectName:             getenv(ProjectEnv),
		CacheRepoUrl:            getenv(CacheRepoEnv),
		GitTokenParameterPrefix: strings.TrimSuffix(getenv(GitTokenPrefixEnv), "/"),
	}
	var missing []string
	if c.ProjectName == "" {
		missing = append(missing, ProjectEnv)
	}
	if c.CacheRepoUrl == "" {
		missing = append(missing, CacheRepoEnv)
	}
	if c.GitTokenParameterPrefix == "" {
		missing = append(missing, GitTokenPrefixEnv)
	}
	if len(missing) > 0 {
		return c, fmt.Errorf("codebuild image builder is selected but %s not set", strings.Join(missing, ", "))
	}
	return c, nil
}

// Request is one App Store image build.
type Request struct {
	DeploymentId      string
	ApplicationId     string // the version's uuid, as for the kaniko task
	ApplicationsTable string // the versions table, for the status lambda
	SourceUrl         string
	Tag               string
	Destination       string // <appstore-private>:<hash>-<tag>
	StorageGiB        int32  // APP_STORAGE; 0 = default
}

// GitContext is the BuildKit git context for a source URL at a tag. BuildKit
// clones an https URL only when it ends in .git (it downloads it otherwise).
func GitContext(sourceUrl, tag string) string {
	url := strings.TrimSuffix(strings.TrimSpace(sourceUrl), "/")
	if strings.HasPrefix(url, "git://") {
		url = "https://" + strings.TrimPrefix(url, "git://")
	}
	if !strings.HasSuffix(url, ".git") {
		url += ".git"
	}
	return fmt.Sprintf("%s#refs/tags/%s", url, tag)
}

// CacheRef is the app's own registry cache: one tag per source URL in the
// cache repository, never shared between apps. The key is a sha256 of the
// source URL (the App Store's identity of an app), not the 32-bit hash in the
// image tags, so two apps cannot collide on a cache.
func CacheRef(cacheRepoUrl, sourceUrl string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(sourceUrl)))
	return fmt.Sprintf("%s:app-%s", cacheRepoUrl, hex.EncodeToString(sum[:])[:defaultCacheKeySize])
}

// ComputeTypeOverride returns a larger compute type when the build needs
// more disk than the project's default (BUILD_GENERAL1_LARGE, 128 GB).
func ComputeTypeOverride(storageGiB int32) (cbtypes.ComputeType, bool) {
	if storageGiB > largeDiskGiB {
		return cbtypes.ComputeTypeBuildGeneral1Xlarge, true
	}
	return "", false
}

// GitTokenParameterName is where a build's GitHub token is put in SSM.
func GitTokenParameterName(prefix, deploymentId string) string {
	return fmt.Sprintf("%s/%s/git-token", prefix, deploymentId)
}

// StartBuildInput builds the StartBuild call. gitTokenParameter is the SSM
// parameter holding a private repo's token ("" for a public repo); the build
// gets it as GIT_TOKEN through CodeBuild's PARAMETER_STORE variable type, so
// the token never shows in the build's recorded variables.
func StartBuildInput(cfg Config, r Request, gitTokenParameter string) *codebuild.StartBuildInput {
	vars := []cbtypes.EnvironmentVariable{
		plain(ContextVar, GitContext(r.SourceUrl, r.Tag)),
		plain(DestinationVar, r.Destination),
		plain(CacheRefVar, CacheRef(cfg.CacheRepoUrl, r.SourceUrl)),
		plain(DeploymentIdVar, r.DeploymentId),
		plain(ApplicationIdVar, r.ApplicationId),
	}
	if r.ApplicationsTable != "" {
		vars = append(vars, plain(ApplicationsTableVar, r.ApplicationsTable))
	}
	if gitTokenParameter != "" {
		vars = append(vars, cbtypes.EnvironmentVariable{
			Name:  aws.String(GitTokenVar),
			Value: aws.String(gitTokenParameter),
			Type:  cbtypes.EnvironmentVariableTypeParameterStore,
		})
	}
	in := &codebuild.StartBuildInput{
		ProjectName:                  aws.String(cfg.ProjectName),
		EnvironmentVariablesOverride: vars,
		// A retried provisioner must not start a second build for the
		// same deployment.
		IdempotencyToken: aws.String(r.DeploymentId),
	}
	if computeType, ok := ComputeTypeOverride(r.StorageGiB); ok {
		in.ComputeTypeOverride = computeType
	}
	return in
}

func plain(name, value string) cbtypes.EnvironmentVariable {
	return cbtypes.EnvironmentVariable{
		Name:  aws.String(name),
		Value: aws.String(value),
		Type:  cbtypes.EnvironmentVariableTypePlaintext,
	}
}

// CodeBuildAPI is the part of the CodeBuild client used here.
type CodeBuildAPI interface {
	StartBuild(ctx context.Context, params *codebuild.StartBuildInput, optFns ...func(*codebuild.Options)) (*codebuild.StartBuildOutput, error)
}

// SSMAPI is the part of the SSM client used here.
type SSMAPI interface {
	PutParameter(ctx context.Context, params *ssm.PutParameterInput, optFns ...func(*ssm.Options)) (*ssm.PutParameterOutput, error)
	DeleteParameter(ctx context.Context, params *ssm.DeleteParameterInput, optFns ...func(*ssm.Options)) (*ssm.DeleteParameterOutput, error)
}

// Start puts the GitHub token (if any) in SSM and starts the build. It
// returns the build's ARN. The status lambda deletes the token parameter
// when the build ends; Start deletes it itself if the build doesn't start.
func Start(ctx context.Context, cb CodeBuildAPI, params SSMAPI, cfg Config, r Request, gitToken string) (string, error) {
	tokenParameter := ""
	if gitToken != "" {
		tokenParameter = GitTokenParameterName(cfg.GitTokenParameterPrefix, r.DeploymentId)
		if _, err := params.PutParameter(ctx, &ssm.PutParameterInput{
			Name:        aws.String(tokenParameter),
			Value:       aws.String(gitToken),
			Type:        ssmtypes.ParameterTypeSecureString,
			Overwrite:   aws.Bool(true),
			Description: aws.String("GitHub token for App Store image build " + r.DeploymentId + "; deleted when the build ends"),
		}); err != nil {
			return "", fmt.Errorf("error storing the build's GitHub token: %w", err)
		}
	}

	out, err := cb.StartBuild(ctx, StartBuildInput(cfg, r, tokenParameter))
	if err != nil {
		if tokenParameter != "" {
			_, _ = params.DeleteParameter(ctx, &ssm.DeleteParameterInput{Name: aws.String(tokenParameter)})
		}
		return "", fmt.Errorf("error starting image build in %s: %w", cfg.ProjectName, err)
	}
	if out.Build == nil {
		return "", nil
	}
	return aws.ToString(out.Build.Arn), nil
}

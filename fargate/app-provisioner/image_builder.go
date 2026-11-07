package main

import (
	"context"
	"log"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/codebuild"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/pennsieve/app-deploy-service/app-provisioner/provisioner/imagebuild"
)

// buildStarter starts an App Store image build: a CodeBuild build
// (docs/builds.md), which pushes the image to <appstore-private>:<hash>-<tag>;
// the status lambda records how the build ended.
type buildStarter interface {
	Name() string
	Start(ctx context.Context, r imagebuild.Request, gitToken string) error
}

// newBuildStarter is the CodeBuild builder. IMAGE_BUILDER, when set, must
// say codebuild, and its configuration must be there.
func newBuildStarter(builder string, getenv func(string) string, cfg aws.Config) (buildStarter, error) {
	if _, err := imagebuild.Selected(builder); err != nil {
		return nil, err
	}
	cbCfg, err := imagebuild.ConfigFromEnv(getenv)
	if err != nil {
		return nil, err
	}
	return codeBuildStarter{
		cfg:       cbCfg,
		codeBuild: codebuild.NewFromConfig(cfg),
		params:    ssm.NewFromConfig(cfg),
	}, nil
}

type codeBuildStarter struct {
	cfg       imagebuild.Config
	codeBuild imagebuild.CodeBuildAPI
	params    imagebuild.SSMAPI
}

func (codeBuildStarter) Name() string { return imagebuild.BuilderCodeBuild }

func (c codeBuildStarter) Start(ctx context.Context, r imagebuild.Request, gitToken string) error {
	arn, err := imagebuild.Start(ctx, c.codeBuild, c.params, c.cfg, r, gitToken)
	if err != nil {
		return err
	}
	log.Printf("started image build %s for deployment %s", arn, r.DeploymentId)
	return nil
}

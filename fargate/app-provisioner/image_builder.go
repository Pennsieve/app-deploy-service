package main

import (
	"context"
	"log"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/codebuild"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/pennsieve/app-deploy-service/app-provisioner/provisioner"
	"github.com/pennsieve/app-deploy-service/app-provisioner/provisioner/imagebuild"
)

// buildStarter starts an App Store image build: the kaniko Fargate task or a
// CodeBuild build, as the environment's image_builder selects (docs/builds.md).
// Either way the image is pushed to the same <appstore-private>:<hash>-<tag>
// and the status lambda records how the build ended.
type buildStarter interface {
	Name() string
	Start(ctx context.Context, r imagebuild.Request, gitToken string) error
}

// newBuildStarter picks the builder from IMAGE_BUILDER: kaniko unless it says
// codebuild. Selecting codebuild without its configuration is an error.
func newBuildStarter(builder string, getenv func(string) string, cfg aws.Config, appProvisioner provisioner.Provisioner) (buildStarter, error) {
	selected, err := imagebuild.Selected(builder)
	if err != nil {
		return nil, err
	}
	if selected == imagebuild.BuilderKaniko {
		return kanikoStarter{appProvisioner: appProvisioner, ecsClient: ecs.NewFromConfig(cfg)}, nil
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

type kanikoStarter struct {
	appProvisioner provisioner.Provisioner
	ecsClient      *ecs.Client
}

func (kanikoStarter) Name() string { return imagebuild.BuilderKaniko }

func (k kanikoStarter) Start(ctx context.Context, r imagebuild.Request, gitToken string) error {
	if err := r.Validate(); err != nil {
		return err
	}
	return PrivateDeploy(ctx, r, gitToken, k.appProvisioner, k.ecsClient)
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

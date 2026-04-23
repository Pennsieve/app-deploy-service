package provisioner

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os/exec"
	"strconv"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/pennsieve/app-deploy-service/app-provisioner/provisioner"
	"github.com/pennsieve/app-deploy-service/app-provisioner/provisioner/utils"
)

type s3BackendAPI interface {
	HeadBucket(ctx context.Context, params *s3.HeadBucketInput, optFns ...func(*s3.Options)) (*s3.HeadBucketOutput, error)
	CreateBucket(ctx context.Context, params *s3.CreateBucketInput, optFns ...func(*s3.Options)) (*s3.CreateBucketOutput, error)
	PutBucketVersioning(ctx context.Context, params *s3.PutBucketVersioningInput, optFns ...func(*s3.Options)) (*s3.PutBucketVersioningOutput, error)
}

type AWSProvisioner struct {
	Config              aws.Config
	AccountId           string
	BackendExists       bool
	Action              string
	Env                 string
	GitUrl              string
	ComputeNodeEfsId    string
	ComputeNodeUuid     string
	AppSlug             string
	RunOnGPU            bool
	RoleName            string
}

func NewAWSProvisioner(cfg aws.Config, accountId string, action string, env string, gitUrl string, computeNodeEfsId string, computeNodeUuid string, app_slug string, runOnGPU bool, roleName string) provisioner.Provisioner {
	return &AWSProvisioner{Config: cfg, AccountId: accountId, Action: action, Env: env, GitUrl: gitUrl, ComputeNodeEfsId: computeNodeEfsId, ComputeNodeUuid: computeNodeUuid, AppSlug: app_slug, RunOnGPU: runOnGPU, RoleName: roleName}
}

func (p *AWSProvisioner) AssumeRole(ctx context.Context) (aws.Credentials, error) {
	log.Printf("assuming role %s ...", p.RoleName)

	stsClient := sts.NewFromConfig(p.Config)

	roleArn := fmt.Sprintf("arn:aws:iam::%s:role/%s", p.AccountId, p.RoleName)
	appCreds := stscreds.NewAssumeRoleProvider(stsClient, roleArn)
	credentials, err := appCreds.Retrieve(ctx)
	if err != nil {
		return aws.Credentials{}, err
	}

	return credentials, nil
}

func (p *AWSProvisioner) GetProvisionerCreds(ctx context.Context) (aws.Credentials, error) {
	log.Println("getting provisioner credentials ...")

	credentials, err := p.Config.Credentials.Retrieve(ctx)
	if err != nil {
		return aws.Credentials{}, err
	}

	return credentials, nil
}

func (p *AWSProvisioner) Create(ctx context.Context) error {
	log.Println("creating infrastructure ...")

	creds, err := p.AssumeRole(ctx)
	if err != nil {
		return err
	}
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return err
	}

	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.Credentials = credentials.NewStaticCredentialsProvider(creds.AccessKeyID, creds.SecretAccessKey, creds.SessionToken)
	})

	bucket := fmt.Sprintf("tfstate-%s", p.AccountId)
	if err := ensureBackendBucket(ctx, client, bucket, cfg.Region); err != nil {
		return err
	}
	p.BackendExists = true

	// create infrastructure
	runOnGPUStr := strconv.FormatBool(p.RunOnGPU)
	cmd := exec.Command("/bin/sh", "/usr/src/app/scripts/infrastructure.sh",
		p.AccountId, creds.AccessKeyID, creds.SecretAccessKey, creds.SessionToken, p.GitUrl, p.ComputeNodeEfsId, p.AppSlug, runOnGPUStr, p.ComputeNodeUuid)
	out, err := cmd.Output()
	if err != nil {
		return err
	}
	fmt.Println(string(out))

	return nil
}

func (p *AWSProvisioner) Delete(ctx context.Context) error {
	fmt.Println("destroying infrastructure")

	creds, err := p.AssumeRole(ctx)
	if err != nil {
		return err
	}
	runOnGPUStr := strconv.FormatBool(p.RunOnGPU)
	cmd := exec.Command("/bin/sh", "/usr/src/app/scripts/destroy-infrastructure.sh",
		p.AccountId, creds.AccessKeyID, creds.SecretAccessKey, creds.SessionToken, p.GitUrl, p.ComputeNodeEfsId, p.AppSlug, runOnGPUStr, p.ComputeNodeUuid)
	out, err := cmd.Output()
	if err != nil {
		return err
	}
	fmt.Println(string(out))

	return nil
}

func (p *AWSProvisioner) CreatePublicRepository(ctx context.Context) error {
	log.Println("creating public repository ...")

	// TODO: validate if repository already exists
	// create public repository
	sourceUrlHash := utils.GenerateHash(p.GitUrl)
	cmd := exec.Command("/bin/sh", "/usr/src/app/scripts/public-repository.sh",
		p.GitUrl, strconv.Itoa(int(sourceUrlHash)))
	out, err := cmd.Output()
	if err != nil {
		return err
	}
	fmt.Println(string(out))

	return nil
}

func ensureBackendBucket(ctx context.Context, client s3BackendAPI, bucket, region string) error {
	_, err := client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(bucket)})
	if err == nil {
		return nil
	}

	var notFound *s3types.NotFound
	var noSuchBucket *s3types.NoSuchBucket
	if !errors.As(err, &notFound) && !errors.As(err, &noSuchBucket) {
		return fmt.Errorf("checking backend bucket %s: %w", bucket, err)
	}

	log.Printf("backend bucket %s not found, creating ...", bucket)

	createInput := &s3.CreateBucketInput{Bucket: aws.String(bucket)}
	if region != "" && region != "us-east-1" {
		createInput.CreateBucketConfiguration = &s3types.CreateBucketConfiguration{
			LocationConstraint: s3types.BucketLocationConstraint(region),
		}
	}
	if _, err := client.CreateBucket(ctx, createInput); err != nil {
		var alreadyOwned *s3types.BucketAlreadyOwnedByYou
		if !errors.As(err, &alreadyOwned) {
			return fmt.Errorf("creating backend bucket %s: %w", bucket, err)
		}
	}

	if _, err := client.PutBucketVersioning(ctx, &s3.PutBucketVersioningInput{
		Bucket: aws.String(bucket),
		VersioningConfiguration: &s3types.VersioningConfiguration{
			Status: s3types.BucketVersioningStatusEnabled,
		},
	}); err != nil {
		return fmt.Errorf("enabling versioning on %s: %w", bucket, err)
	}

	log.Printf("backend bucket %s ready", bucket)
	return nil
}


package provisioner

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"strconv"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/pennsieve/app-deploy-service/app-provisioner/provisioner"
	"github.com/pennsieve/app-deploy-service/app-provisioner/provisioner/utils"
)

type AWSProvisioner struct {
	Config           aws.Config
	AccountId        string
	BackendExists    bool
	Action           string
	Env              string
	GitUrl           string
	ComputeNodeEfsId string
	AppSlug          string
	RunOnGPU         bool
	RoleName         string
}

func NewAWSProvisioner(cfg aws.Config, accountId string, action string, env string, gitUrl string, computeNodeEfsId string, app_slug string, runOnGPU bool, roleName string) provisioner.Provisioner {
	return &AWSProvisioner{Config: cfg, AccountId: accountId, Action: action, Env: env, GitUrl: gitUrl, ComputeNodeEfsId: computeNodeEfsId, AppSlug: app_slug, RunOnGPU: runOnGPU, RoleName: roleName}
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

	// check for backend bucket
	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.Credentials = credentials.NewStaticCredentialsProvider(creds.AccessKeyID, creds.SecretAccessKey, creds.SessionToken)
	})
	resp, err := client.ListBuckets(ctx, &s3.ListBucketsInput{})
	if err != nil {
		return err
	}

	for _, b := range resp.Buckets {
		if *b.Name == fmt.Sprintf("tfstate-%s", p.AccountId) {
			p.BackendExists = true
			break
		}
	}

	if !p.BackendExists {
		// create s3 backend bucket
		return fmt.Errorf("expected tfstate-%s to exist", p.AccountId)
	}

	// create infrastructure
	runOnGPUStr := strconv.FormatBool(p.RunOnGPU)
	cmd := exec.Command("/bin/sh", "/usr/src/app/scripts/infrastructure.sh",
		p.AccountId, creds.AccessKeyID, creds.SecretAccessKey, creds.SessionToken, p.GitUrl, p.ComputeNodeEfsId, p.AppSlug, runOnGPUStr)
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
		p.AccountId, creds.AccessKeyID, creds.SecretAccessKey, creds.SessionToken, p.GitUrl, p.ComputeNodeEfsId, p.AppSlug, runOnGPUStr)
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


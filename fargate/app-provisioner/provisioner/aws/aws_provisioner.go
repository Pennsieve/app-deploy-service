package provisioner

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/service/iam"
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

func (p *AWSProvisioner) CreatePolicy(ctx context.Context) error {
	log.Println("creating an inline policy ...")
	iamClient := iam.NewFromConfig(p.Config)

	policyDoc := fmt.Sprintf(`{
					"Version": "2012-10-17",
					"Statement": [
						{
							"Effect": "Allow",
							"Action": "sts:AssumeRole",
							"Resource": "arn:aws:iam::%s:role/%s"
						}
					]
				}`, p.AccountId, p.RoleName)

	output, err := iamClient.PutRolePolicy(context.Background(), &iam.PutRolePolicyInput{
		PolicyName:     aws.String(fmt.Sprintf("ExternalAccountInlinePolicy-%s", p.AccountId)),
		PolicyDocument: aws.String(policyDoc),
		RoleName:       aws.String(fmt.Sprintf("%s-app-deploy-service-fargate-task-role-use1", p.Env)),
	})
	if err != nil {
		return err
	}

	fmt.Println(output)
	// wait for policy to be attached
	time.Sleep(25 * time.Second)

	return nil
}

func (p *AWSProvisioner) GetPolicy(ctx context.Context) (*string, error) {
	log.Println("getting policy ...")

	iamClient := iam.NewFromConfig(p.Config)

	output, err := iamClient.GetRolePolicy(context.Background(), &iam.GetRolePolicyInput{
		PolicyName: aws.String(fmt.Sprintf("ExternalAccountInlinePolicy-%s", p.AccountId)),
		RoleName:   aws.String(fmt.Sprintf("%s-app-deploy-service-fargate-task-role-use1", p.Env)),
	})
	if err != nil {
		return nil, err
	}

	fmt.Printf("%v", output.PolicyDocument)
	return output.PolicyDocument, err
}

func (p *AWSProvisioner) Create(ctx context.Context) error {
	log.Println("creating infrastructure ...")

	if err := p.CreatePolicy(context.Background()); err != nil {
		return fmt.Errorf("error creating/updating inline policy for account %s: %w", p.AccountId, err)
	}

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

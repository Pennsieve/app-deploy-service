package provisioner

import (
	"context"
	"fmt"
	"log"
	"os/exec"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/pennsieve/app-deploy-service/app-provisioner/provisioner"
)

type AWSProvisioner struct {
	IAMClient     *iam.Client
	STSClient     *sts.Client
	AccountId     string
	BackendExists bool
	Action        string
	Env           string
	GitUrl        string
}

func NewAWSProvisioner(iamClient *iam.Client, stsClient *sts.Client, accountId string, action string, env string, gitUrl string) provisioner.Provisioner {
	return &AWSProvisioner{IAMClient: iamClient, STSClient: stsClient,
		AccountId: accountId, Action: action, Env: env, GitUrl: gitUrl}
}

func (p *AWSProvisioner) Run(ctx context.Context) error {
	log.Println("Starting to provision infrastructure ...")

	switch p.Action {
	case "CREATE":
		return p.create(ctx)
	case "DELETE":
		return p.delete(ctx)
	default:
		return fmt.Errorf("action not supported: %s", p.Action)
	}

}

func (p *AWSProvisioner) assumeRole(ctx context.Context) (aws.Credentials, error) {
	log.Println("assuming role ...")

	provisionerAccountId, err := p.STSClient.GetCallerIdentity(ctx,
		&sts.GetCallerIdentityInput{})
	if err != nil {
		return aws.Credentials{}, err
	}

	roleArn := fmt.Sprintf("arn:aws:iam::%s:role/ROLE-%s", p.AccountId, *provisionerAccountId.Account)
	appCreds := stscreds.NewAssumeRoleProvider(p.STSClient, roleArn)
	credentials, err := appCreds.Retrieve(ctx)
	if err != nil {
		return aws.Credentials{}, err
	}

	return credentials, nil
}

func (p *AWSProvisioner) create(ctx context.Context) error {
	log.Println("creating infrastructure ...")

	creds, err := p.assumeRole(ctx)
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
	cmd := exec.Command("/bin/sh", "/usr/src/app/scripts/infrastructure.sh",
		p.AccountId, creds.AccessKeyID, creds.SecretAccessKey, creds.SessionToken, p.GitUrl)
	out, err := cmd.Output()
	if err != nil {
		return err
	}
	fmt.Println(string(out))

	return nil
}

func (p *AWSProvisioner) delete(ctx context.Context) error {
	fmt.Println("destroying infrastructure")

	creds, err := p.assumeRole(ctx)
	if err != nil {
		return err
	}
	cmd := exec.Command("/bin/sh", "/usr/src/app/scripts/destroy-infrastructure.sh",
		p.AccountId, creds.AccessKeyID, creds.SecretAccessKey, creds.SessionToken, p.GitUrl)
	out, err := cmd.Output()
	if err != nil {
		return err
	}
	fmt.Println(string(out))

	return nil
}

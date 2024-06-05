package provisioner

import (
	"context"
	"fmt"
	"log"
	"os/exec"
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
}

func NewAWSProvisioner(cfg aws.Config, accountId string, action string, env string, gitUrl string, computeNodeEfsId string, app_slug string) provisioner.Provisioner {
	return &AWSProvisioner{Config: cfg, AccountId: accountId, Action: action, Env: env, GitUrl: gitUrl, ComputeNodeEfsId: computeNodeEfsId, AppSlug: app_slug}
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

func (p *AWSProvisioner) AssumeRole(ctx context.Context) (aws.Credentials, error) {
	log.Println("assuming role ...")

	stsClient := sts.NewFromConfig(p.Config)

	provisionerAccountId, err := stsClient.GetCallerIdentity(ctx,
		&sts.GetCallerIdentityInput{})
	if err != nil {
		return aws.Credentials{}, err
	}

	roleArn := fmt.Sprintf("arn:aws:iam::%s:role/ROLE-%s", p.AccountId, *provisionerAccountId.Account)
	appCreds := stscreds.NewAssumeRoleProvider(stsClient, roleArn)
	credentials, err := appCreds.Retrieve(ctx)
	if err != nil {
		return aws.Credentials{}, err
	}

	return credentials, nil
}

func (p *AWSProvisioner) CreatePolicy(ctx context.Context) error {
	log.Println("creating an inline policy ...")
	stsClient := sts.NewFromConfig(p.Config)
	iamClient := iam.NewFromConfig(p.Config)

	provisionerAccountId, err := stsClient.GetCallerIdentity(ctx,
		&sts.GetCallerIdentityInput{})
	if err != nil {
		return err
	}

	policyDoc := fmt.Sprintf(`{
					"Version": "2012-10-17",
					"Statement": [
						{
							"Effect": "Allow",
							"Action": "sts:AssumeRole",
							"Resource": "arn:aws:iam::%s:role/ROLE-%s"
						}
					]
				}`, p.AccountId, *provisionerAccountId.Account)

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

func (p *AWSProvisioner) create(ctx context.Context) error {
	log.Println("creating infrastructure ...")

	_, err := p.GetPolicy(context.Background())
	if err != nil {
		if strings.Contains(err.Error(), "NoSuchEntity") {
			log.Printf("no inline policy exists for account: %s, creating ...", p.AccountId)
			err = p.CreatePolicy(context.Background())
			if err != nil {
				return err
			}
		} else {
			return err
		}
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
	cmd := exec.Command("/bin/sh", "/usr/src/app/scripts/infrastructure.sh",
		p.AccountId, creds.AccessKeyID, creds.SecretAccessKey, creds.SessionToken, p.GitUrl, p.ComputeNodeEfsId, p.AppSlug)
	out, err := cmd.Output()
	if err != nil {
		return err
	}
	fmt.Println(string(out))

	return nil
}

func (p *AWSProvisioner) delete(ctx context.Context) error {
	fmt.Println("destroying infrastructure")

	creds, err := p.AssumeRole(ctx)
	if err != nil {
		return err
	}
	cmd := exec.Command("/bin/sh", "/usr/src/app/scripts/destroy-infrastructure.sh",
		p.AccountId, creds.AccessKeyID, creds.SecretAccessKey, creds.SessionToken, p.GitUrl, p.ComputeNodeEfsId, p.AppSlug)
	out, err := cmd.Output()
	if err != nil {
		return err
	}
	fmt.Println(string(out))

	return nil
}

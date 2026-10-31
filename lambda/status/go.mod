module github.com/pennsieve/app-deploy-service/status

go 1.27.2

require (
	github.com/aws/aws-lambda-go v1.47.0
	github.com/aws/aws-sdk-go-v2 v1.47.5
	github.com/aws/aws-sdk-go-v2/config v1.32.25
	github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue v1.20.48
	github.com/aws/aws-sdk-go-v2/feature/dynamodb/expression v1.7.63
	github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs v1.90.3
	github.com/aws/aws-sdk-go-v2/service/codebuild v1.78.7
	github.com/aws/aws-sdk-go-v2/service/dynamodb v1.59.0
	github.com/aws/aws-sdk-go-v2/service/ecr v1.56.0
	github.com/aws/aws-sdk-go-v2/service/ecs v1.53.8
	github.com/aws/aws-sdk-go-v2/service/ssm v1.56.9
	github.com/google/uuid v1.6.0
	github.com/pennsieve/pennsieve-go-core v1.21.0
	github.com/pusher/pusher-http-go/v5 v5.1.1
	github.com/stretchr/testify v1.12.1
)

require (
	github.com/aws/aws-sdk-go-v2/aws/protocol/eventstream v1.7.25 // indirect
	github.com/aws/aws-sdk-go-v2/credentials v1.19.24 // indirect
	github.com/aws/aws-sdk-go-v2/feature/ec2/imds v1.18.29 // indirect
	github.com/aws/aws-sdk-go-v2/internal/configsources v1.5.8 // indirect
	github.com/aws/aws-sdk-go-v2/internal/endpoints/v2 v2.8.8 // indirect
	github.com/aws/aws-sdk-go-v2/internal/v4a v1.4.30 // indirect
	github.com/aws/aws-sdk-go-v2/service/dynamodbstreams v1.34.0 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/accept-encoding v1.13.12 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/endpoint-discovery v1.12.6 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/presigned-url v1.13.29 // indirect
	github.com/aws/aws-sdk-go-v2/service/signin v1.2.0 // indirect
	github.com/aws/aws-sdk-go-v2/service/sso v1.31.3 // indirect
	github.com/aws/aws-sdk-go-v2/service/ssooidc v1.36.6 // indirect
	github.com/aws/aws-sdk-go-v2/service/sts v1.43.3 // indirect
	github.com/aws/smithy-go v1.28.7 // indirect
	github.com/jmespath/go-jmespath v0.4.0 // indirect
	github.com/sirupsen/logrus v1.10.2 // indirect
	go.yaml.in/yaml/v3 v3.0.5 // indirect
	golang.org/x/crypto v0.58.0 // indirect
	golang.org/x/sys v0.49.0 // indirect
)

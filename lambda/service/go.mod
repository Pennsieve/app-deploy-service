module github.com/pennsieve/app-deploy-service/service

go 1.22

toolchain go1.23.4

require (
	github.com/aws/aws-lambda-go v1.47.0
	github.com/aws/aws-sdk-go-v2 v1.35.0
	github.com/aws/aws-sdk-go-v2/config v1.27.15
	github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue v1.13.17
	github.com/aws/aws-sdk-go-v2/feature/dynamodb/expression v1.7.17
	github.com/aws/aws-sdk-go-v2/service/dynamodb v1.32.3
	github.com/aws/aws-sdk-go-v2/service/ecs v1.41.10
	github.com/aws/aws-sdk-go-v2/service/ssm v1.56.9
	github.com/google/uuid v1.3.0
	github.com/pennsieve/pennsieve-go-core v1.13.0
	github.com/pusher/pusher-http-go/v5 v5.1.1
	github.com/stretchr/testify v1.9.0
)

require (
	github.com/aws/aws-sdk-go-v2/credentials v1.17.15 // indirect
	github.com/aws/aws-sdk-go-v2/feature/ec2/imds v1.16.3 // indirect
	github.com/aws/aws-sdk-go-v2/internal/configsources v1.3.30 // indirect
	github.com/aws/aws-sdk-go-v2/internal/endpoints/v2 v2.6.30 // indirect
	github.com/aws/aws-sdk-go-v2/internal/ini v1.8.0 // indirect
	github.com/aws/aws-sdk-go-v2/service/dynamodbstreams v1.20.7 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/accept-encoding v1.11.2 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/endpoint-discovery v1.9.8 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/presigned-url v1.11.9 // indirect
	github.com/aws/aws-sdk-go-v2/service/sso v1.20.8 // indirect
	github.com/aws/aws-sdk-go-v2/service/ssooidc v1.24.2 // indirect
	github.com/aws/aws-sdk-go-v2/service/sts v1.28.9 // indirect
	github.com/aws/smithy-go v1.22.2 // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/golang-jwt/jwt/v5 v5.2.1 // indirect
	github.com/jmespath/go-jmespath v0.4.0 // indirect
	github.com/kr/text v0.2.0 // indirect
	github.com/lib/pq v1.10.7 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/rogpeppe/go-internal v1.13.1 // indirect
	github.com/sirupsen/logrus v1.9.0 // indirect
	golang.org/x/crypto v0.17.0 // indirect
	golang.org/x/sys v0.21.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

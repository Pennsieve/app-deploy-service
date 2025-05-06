package parser

type OutputValue struct {
	Value string `json:"value"`
}

type Output struct {
	AppEcrUrl        OutputValue `json:"app_ecr_repository"`
	AppTaskDefn      OutputValue `json:"app_id"`
	AppContainerName OutputValue `json:"app_container_name"`
	AppPublicEcrUrl  OutputValue `json:"app_public_ecr_repository"`
	AppPublicEcrArn  OutputValue `json:"app_public_ecr_repository_arn"`
}

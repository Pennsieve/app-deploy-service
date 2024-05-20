package parser

type OutputValue struct {
	Value string `json:"value"`
}

type Output struct {
	AppEcrUrl OutputValue `json:"app_ecr_repository"`
}

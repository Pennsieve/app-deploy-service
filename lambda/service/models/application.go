package models

type Application struct {
	Uuid            string               `json:"uuid"`
	Name            string               `json:"name"`
	Description     string               `json:"description"`
	Resources       ApplicationResources `json:"resources"`
	Account         Account              `json:"account"`
	ComputeNode     ComputeNode          `json:"computeNode"`
	Source          Source               `json:"source"`
	Destination     Destination          `json:"destination"`
	ApplicationType string               `json:"applicationType"`
	Env             string               `json:"environment"`
	OrganizationId  string               `json:"organizationId"`
	UserId          string               `json:"userId"`
	CreatedAt       string               `json:"createdAt"`
}

type Account struct {
	Uuid        string `json:"uuid"`
	AccountId   string `json:"accountId"`
	AccountType string `json:"accountType"`
}

type ComputeNode struct {
	Uuid  string `json:"uuid"`
	EfsId string `json:"efsId"`
}

type Source struct {
	SourceType string `json:"sourceType"`
	Url        string `json:"url"`
}

type Destination struct {
	DestinationType string `json:"destinationType"`
	Url             string `json:"url"`
}

type ApplicationResources struct {
	CPU    int `json:"cpu"`
	Memory int `json:"memory"`
}

package models

type Application struct {
	Uuid                     string               `json:"uuid"`
	ApplicationId            string               `json:"applicationId"`
	ApplicationContainerName string               `json:"applicationContainerName"`
	Name                     string               `json:"name"`
	Description              string               `json:"description"`
	Resources                ApplicationResources `json:"resources"`
	Account                  Account              `json:"account"`
	ComputeNode              ComputeNode          `json:"computeNode"`
	Source                   Source               `json:"source"`
	Destination              Destination          `json:"destination"`
	ApplicationType          string               `json:"applicationType"`
	Env                      string               `json:"environment"`
	OrganizationId           string               `json:"organizationId"`
	UserId                   string               `json:"userId"`
	CreatedAt                string               `json:"createdAt"`
	Params                   interface{}          `json:"params,omitempty"`
	CommandArguments         interface{}          `json:"commandArguments,omitempty"`
	Status                   string               `json:"status"`
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
	SourceType string `json:"type"`
	Url        string `json:"url"`
	Tag        string `json:"tag"`
}

type Destination struct {
	DestinationType string `json:"type"`
	Url             string `json:"url"`
}

type ApplicationResources struct {
	CPU    int `json:"cpu"`
	Memory int `json:"memory"`
}

type ApplicationResponse struct {
	Message string `json:"message"`
}

type RegisterApplicationResponse struct {
	Application  Application `json:"application"`
	DeploymentId string      `json:"deploymentId"`
}

type DeployApplicationResponse struct {
	DeploymentId string `json:"deploymentId"`
}

type AppStoreRegistrationResponse struct {
	RegistrationId string `json:"registrationId"`
}

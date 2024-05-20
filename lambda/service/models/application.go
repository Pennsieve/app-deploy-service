package models

type Application struct {
	Uuid           string `json:"uuid"`
	Name           string `json:"name"`
	Description    string `json:"description"`
	AppEcrUrl      string `json:"workflowManagerUrl"`
	Env            string `json:"environment"`
	CreatedAt      string `json:"createdAt"`
	OrganizationId string `json:"organizationId"`
	UserId         string `json:"userId"`
}

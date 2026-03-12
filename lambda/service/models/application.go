package models

type Application struct {
	Uuid                     string               `json:"uuid"`
	ApplicationId            string               `json:"applicationId"`
	ApplicationContainerName string               `json:"applicationContainerName"`
	Name                     string               `json:"name"`
	Description              string               `json:"description"`
	Resources                ApplicationResources `json:"resources"`                // task level resources
	RunOnGPU                 bool                 `json:"runOnGpu"`                 // container level requirement
	ComputeTypes             []string             `json:"computeTypes,omitempty"`   // supported runtimes, e.g. ["ecs", "lambda"]
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
	Deployments              []Deployment         `json:"deployments"`
	Status                   string               `json:"status"`
}

type AppStoreDeployment struct {
	Source  DeploymentSource `json:"source"`
	Release Release          `json:"release"`
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

type DeploymentSource struct {
	SourceType string `json:"type"`
	Url        string `json:"url"`
	Tag        string `json:"tag"`
	IsPrivate  bool   `json:"isPrivate,omitempty"`
	AuthToken  string `json:"authToken,omitempty"`
}

type Source struct {
	SourceType string `json:"type"`
	Url        string `json:"url"`
}

type Destination struct {
	DestinationType string `json:"type"`
	Url             string `json:"url"`
}

type Release struct {
	ID int `json:"id"`
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

// AppStoreApplication is the API model for an appstore application.
// One per unique sourceUrl. Versions are nested.
type AppStoreApplication struct {
	Uuid       string            `json:"uuid"`
	SourceUrl  string            `json:"sourceUrl"`
	SourceType string            `json:"sourceType"`
	IsPrivate  bool              `json:"isPrivate"`
	Visibility string            `json:"visibility"`
	OwnerId    string            `json:"ownerId"`
	CreatedAt  string            `json:"createdAt"`
	Versions   []AppStoreVersion `json:"versions"`
}

type AppAccess struct {
	EntityId       string `json:"entityId"`
	AppId          string `json:"appId"`
	EntityType     string `json:"entityType"`
	EntityRawId    string `json:"entityRawId"`
	AppUuid        string `json:"appUuid"`
	AccessType     string `json:"accessType"`
	OrganizationId string `json:"organizationId,omitempty"`
	GrantedAt      string `json:"grantedAt"`
	GrantedBy      string `json:"grantedBy"`
}

type AppPermissions struct {
	Visibility string      `json:"visibility"`
	OwnerId    string      `json:"ownerId"`
	Access     []AppAccess `json:"access"`
}

type SetVisibilityRequest struct {
	Visibility string `json:"visibility"`
}

type GrantAccessRequest struct {
	EntityId       string `json:"entityId"`
	OrganizationId string `json:"organizationId,omitempty"`
}

// AppStoreVersion is the API model for a specific version of an appstore application.
type AppStoreVersion struct {
	Uuid           string       `json:"uuid"`
	ApplicationId  string       `json:"applicationId"`
	Version        string       `json:"version"`
	ReleaseId      int          `json:"releaseId"`
	DestinationUrl string       `json:"destinationUrl"`
	CreatedAt      string       `json:"createdAt"`
	Status         string       `json:"status"`
	Deployments    []Deployment `json:"deployments"`
}

// AuthorizeImageResponse is returned by the authorization endpoint.
type AuthorizeImageResponse struct {
	Authorized bool   `json:"authorized"`
	ImageUrl   string `json:"imageUrl,omitempty"`
	Message    string `json:"message,omitempty"`
}

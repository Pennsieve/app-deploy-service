package models

type Application struct {
	Uuid                     string        `json:"uuid"`
	ApplicationId            string        `json:"applicationId"`
	ApplicationContainerName string        `json:"applicationContainerName"`
	Name                     string        `json:"name"`
	Description              string        `json:"description"`
	RuntimeConfig            RuntimeConfig `json:"runtimeConfig"`
	Account                  Account       `json:"account"`
	ComputeNode              ComputeNode   `json:"computeNode"`
	Source                   Source        `json:"source"`
	Destination              Destination   `json:"destination"`
	ApplicationType          string        `json:"applicationType"`
	Env                      string        `json:"environment"`
	OrganizationId           string        `json:"organizationId"`
	UserId                   string        `json:"userId"`
	CreatedAt                string        `json:"createdAt"`
	Params                   interface{}   `json:"params,omitempty"`
	CommandArguments         interface{}   `json:"commandArguments,omitempty"`
	Deployments              []Deployment  `json:"deployments"`
	Status                   string        `json:"status"`
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
	Owner      string `json:"owner,omitempty"`
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

type RuntimeConfig struct {
	CPU          int      `json:"cpu"`
	Memory       int      `json:"memory"`
	ComputeTypes []string `json:"computeTypes,omitempty"`
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
	Uuid             string            `json:"uuid"`
	SourceUrl        string            `json:"sourceUrl"`
	SourceType       string            `json:"sourceType"`
	IsPrivate        bool              `json:"isPrivate"`
	Visibility       string            `json:"visibility"`
	OwnerId          string            `json:"ownerId"`
	CreatedAt        string            `json:"createdAt"`
	Status           AppStoreStatus    `json:"status"`
	LatestVersionTag string            `json:"latestVersionTag,omitempty"`
	Versions         []AppStoreVersion `json:"versions"`
	Params           []AppParameter    `json:"params,omitempty"`
	// Summary is the README's first paragraph as plain text, for cards when
	// app.yml has no description.
	Summary string `json:"summary,omitempty"`
}

// AppParameter is a processor parameter declaration parsed from the app's
// app.yml.
type AppParameter struct {
	Name         string   `json:"name"`
	Type         string   `json:"type,omitempty"`
	Required     bool     `json:"required"`
	Description  string   `json:"description,omitempty"`
	DefaultValue string   `json:"defaultValue,omitempty"`
	ValidValues  []string `json:"validValues,omitempty"`
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

type PatchAppStoreApplicationRequest struct {
	Status AppStoreStatus `json:"status"`
}

type AppStoreSync struct {
	Source DeploymentSource `json:"source"`
}

type SetPermissionsRequest struct {
	Visibility string             `json:"visibility"`
	Users      []PermissionEntity `json:"users,omitempty"`
	Teams      []PermissionEntity `json:"teams,omitempty"`
	Workspaces []PermissionEntity `json:"workspaces,omitempty"`
}

type PermissionEntity struct {
	EntityId       string `json:"entityId"`
	OrganizationId string `json:"organizationId,omitempty"`
}

// AppStoreVersion is the API model for a specific version of an appstore application.
// DestinationUrl is intentionally omitted; it is only exposed via the registry endpoint.
type AppStoreVersion struct {
	Uuid          string       `json:"uuid"`
	ApplicationId string       `json:"applicationId"`
	Version       string       `json:"version"`
	ReleaseId     int          `json:"releaseId"`
	CreatedAt     string       `json:"createdAt"`
	Status        string       `json:"status"`
	Deployments   []Deployment `json:"deployments"`
	// Manifest is this version's app.yml; absent for an app without one.
	Manifest *AppManifest `json:"manifest,omitempty"`
	// ManifestProblems: how its app.yml doesn't follow the schema, as found
	// at registration ("path: reason" each).
	ManifestProblems []string `json:"manifestProblems,omitempty"`
	// License: the repository's license at this version (an SPDX id, from its
	// LICENSE file); absent when there's none or it's unknown.
	License string `json:"license,omitempty"`
}

// AppManifest is a version's app.yml: what the app is, what it asks for, its
// parameters, and what goes in and out.
type AppManifest struct {
	Name        string          `json:"name,omitempty"`
	Description string          `json:"description,omitempty"`
	Tags        []string        `json:"tags,omitempty"`
	Maintainers []AppMaintainer `json:"maintainers,omitempty"`
	Runtime     AppRuntime      `json:"runtime"`
	Parameters  []AppParameter  `json:"parameters"`
	Inputs      []AppPort       `json:"inputs"`
	Outputs     []AppPort       `json:"outputs"`
}

// AppRuntime: CPU units and MiB, compute types and timeout; 0 or empty when
// the manifest doesn't declare them.
type AppRuntime struct {
	CPU            int      `json:"cpu,omitempty"`
	Memory         int      `json:"memory,omitempty"`
	ComputeTypes   []string `json:"computeTypes,omitempty"`
	TimeoutSeconds int      `json:"timeoutSeconds,omitempty"`
	// Interactive: a human-in-the-loop app, run as a live session of
	// SessionType ("jupyter" is a notebook); Kernel is a Jupyter session's
	// language ("python", "r").
	Interactive bool   `json:"interactive,omitempty"`
	SessionType string `json:"sessionType,omitempty"`
	Kernel      string `json:"kernel,omitempty"`
}

type AppMaintainer struct {
	Name  string `json:"name"`
	Email string `json:"email,omitempty"`
	Orcid string `json:"orcid,omitempty"`
}

// AppPort is an input or output: MIME types, EDAM format ids, or neither
// (any file). Container and ConformsTo are omitted when app.yml doesn't set
// them; no container means file.
type AppPort struct {
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	MediaTypes  []string `json:"mediaTypes"`
	Formats     []string `json:"formats,omitempty"`
	Container   string   `json:"container,omitempty"`
	ConformsTo  []string `json:"conformsTo,omitempty"`
	Placement   string   `json:"placement,omitempty"`
}

type AppStoreApplicationDetail struct {
	Uuid             string            `json:"uuid"`
	SourceUrl        string            `json:"sourceUrl"`
	SourceType       string            `json:"sourceType"`
	IsPrivate        bool              `json:"isPrivate"`
	Visibility       string            `json:"visibility"`
	OwnerId          string            `json:"ownerId"`
	CreatedAt        string            `json:"createdAt"`
	Status           AppStoreStatus    `json:"status"`
	LatestVersionTag string            `json:"latestVersionTag,omitempty"`
	Versions         []AppStoreVersion `json:"versions"`
	Assets           map[string]string `json:"assets"`
	Params           []AppParameter    `json:"params,omitempty"`
}

// RegistryImageResponse is returned by the registry endpoint.
type RegistryImageResponse struct {
	Authorized bool   `json:"authorized"`
	ImageUrl   string `json:"imageUrl,omitempty"`
	Message    string `json:"message,omitempty"`
}

package store_dynamodb

import (
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/pennsieve/app-deploy-service/service/models"
)

type Application struct {
	Uuid                     string `dynamodbav:"uuid"`
	Name                     string `dynamodbav:"name"`
	Description              string `dynamodbav:"description"`
	ApplicationType          string `dynamodbav:"applicationType"`
	ApplicationId            string `dynamodbav:"applicationId"`
	ApplicationContainerName string `dynamodbav:"applicationContainerName"`

	AccountUuid string `dynamodbav:"accountUuid"`
	AccountId   string `dynamodbav:"accountId"`
	AccountType string `dynamodbav:"accountType"`

	ComputeNodeUuid  string `dynamodbav:"computeNodeUuid"`
	ComputeNodeEfsId string `dynamodbav:"computeNodeEfsId"`

	SourceType string `dynamodbav:"sourceType"`
	SourceUrl  string `dynamodbav:"sourceUrl"`

	DestinationType string `dynamodbav:"destinationType"`
	DestinationUrl  string `dynamodbav:"destinationUrl"`

	CPU          int      `dynamodbav:"cpu"`
	Memory       int      `dynamodbav:"memory"`
	RunOnGPU     bool     `dynamodbav:"runOnGpu"`
	ComputeTypes []string `dynamodbav:"computeTypes,omitempty"`

	Env string `dynamodbav:"environment"`

	OrganizationId string `dynamodbav:"organizationId"`
	UserId         string `dynamodbav:"userId"`
	CreatedAt      string `dynamodbav:"createdAt"`

	Params           interface{} `dynamodbav:"params"`
	CommandArguments interface{} `dynamodbav:"commandArguments"`

	Status string `dynamodbav:"registrationStatus"`
}

type ApplicationKey struct {
	Uuid string `dynamodbav:"uuid"`
}

func (i Application) GetKey() map[string]types.AttributeValue {
	uuid, err := attributevalue.Marshal(i.Uuid)
	if err != nil {
		panic(err)
	}

	return map[string]types.AttributeValue{"uuid": uuid}
}

// AppStoreApplication represents an application in the appstore.
// One record per unique sourceUrl (the git repository).
type AppStoreApplication struct {
	Uuid       string                `dynamodbav:"uuid"`
	SourceUrl  string                `dynamodbav:"sourceUrl"`
	SourceType string                `dynamodbav:"sourceType"`
	IsPrivate  bool                  `dynamodbav:"isPrivate"`
	Visibility string                `dynamodbav:"visibility"`
	OwnerId    string                `dynamodbav:"ownerId"`
	CreatedAt  string                `dynamodbav:"createdAt"`
	Status     models.AppStoreStatus `dynamodbav:"status,omitempty"`
	// Params are the processor parameter declarations parsed from the app's
	// app.yml at publish time. app.yml is the source of truth; downstream
	// services (e.g. workflow-service) read these as parameter defaults.
	Params []AppParameter `dynamodbav:"params,omitempty"`
	// Summary is the first paragraph of the README at SummaryTag, as plain
	// text: what the app's card shows without an app.yml description.
	// SummaryTag records which release it came from (also when the README
	// had none), so it's computed once per release.
	Summary    string `dynamodbav:"summary,omitempty"`
	SummaryTag string `dynamodbav:"summaryTag,omitempty"`
}

// AppParameter is a single processor parameter declaration from app.yml. The
// attribute names must stay stable — consumers read them by these keys.
type AppParameter struct {
	Name         string   `dynamodbav:"name"`
	Type         string   `dynamodbav:"type,omitempty"`
	Required     bool     `dynamodbav:"required"`
	Description  string   `dynamodbav:"description,omitempty"`
	DefaultValue string   `dynamodbav:"defaultValue,omitempty"`
	ValidValues  []string `dynamodbav:"validValues,omitempty"`
}

type AppAccess struct {
	EntityId       string `dynamodbav:"entityId"`
	AppId          string `dynamodbav:"appId"`
	EntityType     string `dynamodbav:"entityType"`
	EntityRawId    string `dynamodbav:"entityRawId"`
	AppUuid        string `dynamodbav:"appUuid"`
	AccessType     string `dynamodbav:"accessType"`
	OrganizationId string `dynamodbav:"organizationId,omitempty"`
	GrantedAt      string `dynamodbav:"grantedAt"`
	GrantedBy      string `dynamodbav:"grantedBy"`
}

func (i AppStoreApplication) GetKey() map[string]types.AttributeValue {
	uuid, err := attributevalue.Marshal(i.Uuid)
	if err != nil {
		panic(err)
	}

	return map[string]types.AttributeValue{"uuid": uuid}
}

// AppStoreVersion represents a specific version of an appstore application.
// Multiple versions can exist for a single application (sourceUrl).
type AppStoreVersion struct {
	Uuid           string `dynamodbav:"uuid"`
	ApplicationId  string `dynamodbav:"applicationId"`
	Version        string `dynamodbav:"version"`
	ReleaseId      int    `dynamodbav:"releaseId"`
	DestinationUrl string `dynamodbav:"destinationUrl"`
	CreatedAt      string `dynamodbav:"createdAt"`
	Status         string `dynamodbav:"registrationStatus"`
	// Manifest is this version's app.yml, parsed at registration; nil for an
	// app without one, or a version registered before manifests were kept.
	Manifest *AppManifest `dynamodbav:"manifest,omitempty"`
	// ManifestProblems is how the app.yml doesn't follow its schema
	// (manifestschema), one "path: reason" each, as found at registration.
	ManifestProblems []string `dynamodbav:"manifestProblems,omitempty"`
	// License is the repository's license (its SPDX id) at this version, as
	// GitHub reads its LICENSE file; empty when there's none or it's unknown.
	License string `dynamodbav:"license,omitempty"`
	// DefaultVariant, VariantOrder and Variants are set only when the
	// version's app.yml declares variants (docs/app-manifest-variants.md).
	// Manifest is then the default variant's resolved manifest, so readers
	// that don't know about variants see the default. Variants is a map,
	// keyed by variant id, so one variant can be updated on its own.
	DefaultVariant string                `dynamodbav:"defaultVariant,omitempty"`
	VariantOrder   []string              `dynamodbav:"variantOrder,omitempty"`
	Variants       map[string]AppVariant `dynamodbav:"variants,omitempty"`
	// Build is the image's build settings from app.yml's top-level build,
	// for a version without variants (each variant has its own); nil when
	// app.yml has none.
	Build *AppBuild `dynamodbav:"build,omitempty"`
}

// AppVariant is one variant of a version: its label, its resolved manifest
// (the top level with the variant's overrides) and how its image is built.
type AppVariant struct {
	Name        string       `dynamodbav:"name,omitempty"`
	Description string       `dynamodbav:"description,omitempty"`
	Manifest    *AppManifest `dynamodbav:"manifest,omitempty"`
	Build       *AppBuild    `dynamodbav:"build,omitempty"`
}

// AppBuild is how an image is built: Docker build args, the stage to build
// (empty: the last) and the Dockerfile's path in the repository (empty:
// Dockerfile).
type AppBuild struct {
	Args       map[string]string `dynamodbav:"args,omitempty"`
	Target     string            `dynamodbav:"target,omitempty"`
	Dockerfile string            `dynamodbav:"dockerfile,omitempty"`
}

// VersionVariants is what SetVariants records on a version.
type VersionVariants struct {
	DefaultVariant string
	VariantOrder   []string
	Variants       map[string]AppVariant
	Build          *AppBuild
}

// Empty is true for an app.yml without variants or a build: nothing to record.
func (v VersionVariants) Empty() bool {
	return v.DefaultVariant == "" && len(v.VariantOrder) == 0 && len(v.Variants) == 0 && v.Build == nil
}

// AppManifest is a version's app.yml as stored.
type AppManifest struct {
	Name        string          `dynamodbav:"name,omitempty"`
	Description string          `dynamodbav:"description,omitempty"`
	Tags        []string        `dynamodbav:"tags,omitempty"`
	Maintainers []AppMaintainer `dynamodbav:"maintainers,omitempty"`
	Runtime     AppRuntime      `dynamodbav:"runtime"`
	Parameters  []AppParameter  `dynamodbav:"parameters,omitempty"`
	Inputs      []AppPort       `dynamodbav:"inputs,omitempty"`
	Outputs     []AppPort       `dynamodbav:"outputs,omitempty"`
	// Variant names the variant this manifest was resolved for; nil for an
	// app without variants.
	Variant *AppManifestVariant `dynamodbav:"variant,omitempty"`
}

// AppManifestVariant is the variant a resolved manifest belongs to.
type AppManifestVariant struct {
	ID          string `dynamodbav:"id"`
	Name        string `dynamodbav:"name,omitempty"`
	Description string `dynamodbav:"description,omitempty"`
}

// AppRuntime is what a version asks for to run: CPU units and MiB, the
// compute types it runs on, and its timeout. Zero means not declared.
type AppRuntime struct {
	CPU            int      `dynamodbav:"cpu,omitempty"`
	Memory         int      `dynamodbav:"memory,omitempty"`
	ComputeTypes   []string `dynamodbav:"computeTypes,omitempty"`
	TimeoutSeconds int      `dynamodbav:"timeoutSeconds,omitempty"`
	// LLM: the app calls language models through Pennsieve (opt-in to the
	// LLM proxy sidecar); workflow-service reads it at run start.
	LLM bool `dynamodbav:"llm,omitempty"`
	// Interactive: a human-in-the-loop app, run as a live session of
	// SessionType ("jupyter" is a notebook); Kernel is a Jupyter session's
	// language ("python", "r").
	Interactive bool   `dynamodbav:"interactive,omitempty"`
	SessionType string `dynamodbav:"sessionType,omitempty"`
	Kernel      string `dynamodbav:"kernel,omitempty"`
}

// AppMaintainer is one of the people maintaining an app; Orcid is the bare
// iD (0000-0002-1825-0097).
type AppMaintainer struct {
	Name  string `dynamodbav:"name"`
	Email string `dynamodbav:"email,omitempty"`
	Orcid string `dynamodbav:"orcid,omitempty"`
}

// AppPort is one of a version's declared inputs or outputs: by MIME type,
// by EDAM format id (format_3590), or neither, meaning any file. Container
// (file or directory) and ConformsTo (folder structures such as bids@1.9,
// outermost first) are empty when app.yml doesn't set them.
type AppPort struct {
	Name        string   `dynamodbav:"name"`
	Description string   `dynamodbav:"description,omitempty"`
	MediaTypes  []string `dynamodbav:"mediaTypes,omitempty"`
	Formats     []string `dynamodbav:"formats,omitempty"`
	Container   string   `dynamodbav:"container,omitempty"`
	ConformsTo  []string `dynamodbav:"conformsTo,omitempty"`
	Placement   string   `dynamodbav:"placement,omitempty"`
}

func (i AppStoreVersion) GetKey() map[string]types.AttributeValue {
	uuid, err := attributevalue.Marshal(i.Uuid)
	if err != nil {
		panic(err)
	}

	return map[string]types.AttributeValue{"uuid": uuid}
}

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
	// Citation is the repository's CITATION.cff at this version: its authors,
	// how to cite it, and the works it lists typed for DataCite (app.yml's
	// application.relations applied). Nil when there's none or it can't be
	// read; its problems are among ManifestProblems.
	Citation *AppCitation `dynamodbav:"citation,omitempty"`
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
	// Review is app.yml's review block (docs/app-manifest-review.md): how
	// the release is tested before it's made public. Nil when app.yml has
	// none, or none of it could be read (the schema reports why).
	Review *AppReview `dynamodbav:"review,omitempty"`
	// CommitSha is the commit the release tag pointed to at registration, so
	// a review is tied to the code that was built; empty when GitHub
	// couldn't say.
	CommitSha string `dynamodbav:"commitSha,omitempty"`
	// ImageDigest is the digest of the image built for this version (the
	// default variant's, with variants), recorded by the status lambda when
	// the build succeeds; empty until then, or when it couldn't be read.
	ImageDigest string `dynamodbav:"imageDigest,omitempty"`
}

// AppReview is a version's app.yml review block as stored. Parameters are
// kept as written, as strings.
type AppReview struct {
	Version    int                    `dynamodbav:"version"`
	Generate   *ReviewGenerate        `dynamodbav:"generate,omitempty"`
	Inputs     map[string]ReviewInput `dynamodbav:"inputs,omitempty"`
	Parameters map[string]string      `dynamodbav:"parameters,omitempty"`
}

// ReviewGenerate is a command that writes review input, run in a platform
// generator image (Image empty: python), with Source mounted when given.
type ReviewGenerate struct {
	Image   string         `dynamodbav:"image,omitempty"`
	Command string         `dynamodbav:"command,omitempty"`
	Root    string         `dynamodbav:"root,omitempty"`
	Source  *ReviewDataset `dynamodbav:"source,omitempty"`
}

// ReviewInput is where one input port's review data comes from: one of
// Generate, Files or Dataset.
type ReviewInput struct {
	Generate *ReviewGenerate `dynamodbav:"generate,omitempty"`
	Files    []string        `dynamodbav:"files,omitempty"`
	Dataset  *ReviewDataset  `dynamodbav:"dataset,omitempty"`
}

// ReviewDataset is a public dataset by DOI or URL, optionally some of its
// files.
type ReviewDataset struct {
	DOI   string   `dynamodbav:"doi,omitempty"`
	URL   string   `dynamodbav:"url,omitempty"`
	Files []string `dynamodbav:"files,omitempty"`
}

// AppCitation is a version's CITATION.cff as stored.
type AppCitation struct {
	Title        string           `dynamodbav:"title,omitempty"`
	Version      string           `dynamodbav:"version,omitempty"`
	DOI          string           `dynamodbav:"doi,omitempty"`
	DateReleased string           `dynamodbav:"dateReleased,omitempty"`
	Authors      []CitationAuthor `dynamodbav:"authors,omitempty"`
	// PreferredCitation is how the app asks to be cited (the work
	// CITATION.cff's preferred-citation names); nil means cite the software
	// itself (Title, Authors, Version, DOI).
	PreferredCitation *RelatedWork `dynamodbav:"preferredCitation,omitempty"`
	// RelatedWorks is every work it lists, preferred citation first, then
	// references, each once.
	RelatedWorks []RelatedWork `dynamodbav:"relatedWorks,omitempty"`
}

// CitationAuthor is one of CITATION.cff's authors: a person (GivenNames,
// FamilyNames) or an entity (only Name). Name is how it's shown; Orcid is
// the bare iD.
type CitationAuthor struct {
	Name        string `dynamodbav:"name"`
	GivenNames  string `dynamodbav:"givenNames,omitempty"`
	FamilyNames string `dynamodbav:"familyNames,omitempty"`
	Orcid       string `dynamodbav:"orcid,omitempty"`
	Affiliation string `dynamodbav:"affiliation,omitempty"`
}

// RelatedWork is a work CITATION.cff lists with its DataCite relation type.
// DOI is normalised (10.1038/nbt.4314), or empty for a work without one
// (shown by its URL, not sent to DataCite). Source is "preferred-citation"
// or "references".
type RelatedWork struct {
	DOI          string   `dynamodbav:"doi,omitempty"`
	RelationType string   `dynamodbav:"relationType"`
	Title        string   `dynamodbav:"title,omitempty"`
	Authors      []string `dynamodbav:"authors,omitempty"`
	Year         int      `dynamodbav:"year,omitempty"`
	Journal      string   `dynamodbav:"journal,omitempty"`
	URL          string   `dynamodbav:"url,omitempty"`
	Source       string   `dynamodbav:"source"`
}

// AppVariant is one variant of a version: its label, its resolved manifest
// (the top level with the variant's overrides) and how its image is built.
type AppVariant struct {
	Name        string       `dynamodbav:"name,omitempty"`
	Description string       `dynamodbav:"description,omitempty"`
	Manifest    *AppManifest `dynamodbav:"manifest,omitempty"`
	Build       *AppBuild    `dynamodbav:"build,omitempty"`
	// DestinationUrl, Status, DeploymentId and FailureReason are the
	// variant's own build: its image (<hash>-<tag> for the default,
	// <hash>-<tag>--<id> for the others), its status (registering, deployed,
	// error), the deployment record that builds it, and why it failed. They
	// are written by the per-variant builds; until then they are absent, and
	// the registry treats a non-default variant without a status as not yet
	// deployed.
	DestinationUrl string `dynamodbav:"destinationUrl,omitempty"`
	Status         string `dynamodbav:"status,omitempty"`
	DeploymentId   string `dynamodbav:"deploymentId,omitempty"`
	FailureReason  string `dynamodbav:"failureReason,omitempty"`
	// BuildStorageGiB is the disk the variant's build asks for (100 when
	// its computeTypes include gpu); 0 for the builder's default. Set at
	// registration for the provisioner, never shown.
	BuildStorageGiB int32 `dynamodbav:"buildStorageGiB,omitempty"`
	// ImageDigest is the digest of the variant's image, recorded by the
	// status lambda when its build succeeds.
	ImageDigest string `dynamodbav:"imageDigest,omitempty"`
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
	// References are the shared, published data the app reads (app.yml
	// references), in public identifiers; workflow-service resolves them in
	// environments-service's catalog when a run starts.
	References []AppReference `dynamodbav:"references,omitempty"`
}

// AppReference is one app.yml reference as stored.
type AppReference struct {
	Name        string               `dynamodbav:"name"`
	Description string               `dynamodbav:"description,omitempty"`
	Kind        string               `dynamodbav:"kind"`
	AssetClass  string               `dynamodbav:"assetClass"`
	ToolVersion string               `dynamodbav:"toolVersion,omitempty"`
	Choice      string               `dynamodbav:"choice"`
	TaxonomyID  int                  `dynamodbav:"taxonomyId,omitempty"`
	Assembly    string               `dynamodbav:"assembly,omitempty"`
	Seqcol      string               `dynamodbav:"seqcol,omitempty"`
	Annotation  *ReferenceAnnotation `dynamodbav:"annotation,omitempty"`
	Organisms   []int                `dynamodbav:"organisms,omitempty"`
	Default     *ReferenceIdentity   `dynamodbav:"default,omitempty"`
	Source      string               `dynamodbav:"source,omitempty"`
	SHA256      string               `dynamodbav:"sha256,omitempty"`
	License     string               `dynamodbav:"license,omitempty"`
}

// ReferenceAnnotation is an annotation release: GENCODE 50.
type ReferenceAnnotation struct {
	Provider string `dynamodbav:"provider"`
	Version  string `dynamodbav:"version"`
}

// ReferenceIdentity names one reference: a choice: user reference's default.
type ReferenceIdentity struct {
	TaxonomyID int                  `dynamodbav:"taxonomyId,omitempty"`
	Assembly   string               `dynamodbav:"assembly,omitempty"`
	Seqcol     string               `dynamodbav:"seqcol,omitempty"`
	Annotation *ReferenceAnnotation `dynamodbav:"annotation,omitempty"`
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

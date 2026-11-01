package mappers

import (
	"sort"

	"github.com/pennsieve/app-deploy-service/service/models"
	"github.com/pennsieve/app-deploy-service/service/store_dynamodb"
)

func defaultComputeTypes(ct []string) []string {
	if len(ct) == 0 {
		return []string{"standard"}
	}
	return ct
}

func StoreToModel(a store_dynamodb.Application) models.Application {
	return models.Application{
		Uuid:                     a.Uuid,
		ApplicationId:            a.ApplicationId,
		ApplicationContainerName: a.ApplicationContainerName,
		Name:                     a.Name,
		Description:              a.Description,
		RuntimeConfig: models.RuntimeConfig{
			CPU:          a.CPU,
			Memory:       a.Memory,
			ComputeTypes: defaultComputeTypes(a.ComputeTypes),
		},
		ApplicationType: a.ApplicationType,
		Account: models.Account{
			Uuid:        a.AccountUuid,
			AccountId:   a.AccountId,
			AccountType: a.AccountType,
		},
		ComputeNode: models.ComputeNode{
			Uuid:  a.ComputeNodeUuid,
			EfsId: a.ComputeNodeEfsId,
		},
		Source: models.Source{
			SourceType: a.SourceType,
			Url:        a.SourceUrl,
		},
		Destination: models.Destination{
			DestinationType: a.DestinationType,
			Url:             a.DestinationUrl,
		},
		Params:           a.Params,
		CommandArguments: a.CommandArguments,
		Env:              a.Env,
		CreatedAt:        a.CreatedAt,
		OrganizationId:   a.OrganizationId,
		UserId:           a.UserId,
		Status:           a.Status,
	}
}

func DynamoDBApplicationToJsonApplication(dynamoApplications []store_dynamodb.Application) []models.Application {
	applications := []models.Application{}

	for _, a := range dynamoApplications {
		applications = append(applications, StoreToModel(a))
	}

	return applications
}

func AppStoreAppToModel(a store_dynamodb.AppStoreApplication) models.AppStoreApplication {
	status := a.Status
	if status == "" {
		status = models.AppStoreStatusActive
	}
	return models.AppStoreApplication{
		Uuid:       a.Uuid,
		SourceUrl:  a.SourceUrl,
		SourceType: a.SourceType,
		IsPrivate:  a.IsPrivate,
		Visibility: a.Visibility,
		OwnerId:    a.OwnerId,
		CreatedAt:  a.CreatedAt,
		Status:     status,
		Params:     AppParametersToModels(a.Params),
		Summary:    a.Summary,
	}
}

func AppParametersToModels(params []store_dynamodb.AppParameter) []models.AppParameter {
	if len(params) == 0 {
		return nil
	}
	result := make([]models.AppParameter, 0, len(params))
	for _, p := range params {
		result = append(result, models.AppParameter{
			Name:         p.Name,
			Type:         p.Type,
			Required:     p.Required,
			Description:  p.Description,
			DefaultValue: p.DefaultValue,
			ValidValues:  p.ValidValues,
		})
	}
	return result
}

func AppAccessToModel(a store_dynamodb.AppAccess) models.AppAccess {
	return models.AppAccess{
		EntityId:       a.EntityId,
		AppId:          a.AppId,
		EntityType:     a.EntityType,
		EntityRawId:    a.EntityRawId,
		AppUuid:        a.AppUuid,
		AccessType:     a.AccessType,
		OrganizationId: a.OrganizationId,
		GrantedAt:      a.GrantedAt,
		GrantedBy:      a.GrantedBy,
	}
}

func AppAccessItemsToModels(items []store_dynamodb.AppAccess) []models.AppAccess {
	result := []models.AppAccess{}
	for _, a := range items {
		result = append(result, AppAccessToModel(a))
	}
	return result
}

func AppStoreAppsToModels(apps []store_dynamodb.AppStoreApplication) []models.AppStoreApplication {
	result := []models.AppStoreApplication{}
	for _, a := range apps {
		result = append(result, AppStoreAppToModel(a))
	}
	return result
}

func AppStoreVersionToModel(v store_dynamodb.AppStoreVersion) models.AppStoreVersion {
	return models.AppStoreVersion{
		Uuid:             v.Uuid,
		ApplicationId:    v.ApplicationId,
		Version:          v.Version,
		ReleaseId:        v.ReleaseId,
		CreatedAt:        v.CreatedAt,
		Status:           v.Status,
		Manifest:         AppManifestToModel(v.Manifest),
		ManifestProblems: v.ManifestProblems,
		License:          v.License,
		Citation:         AppCitationToModel(v.Citation),
		DefaultVariant:   v.DefaultVariant,
		Variants:         appStoreVariantsToModels(v),
		Review:           AppReviewToModel(v.Review),
		CommitSha:        v.CommitSha,
		ImageDigest:      v.ImageDigest,
	}
}

func AppReviewToModel(r *store_dynamodb.AppReview) *models.AppReview {
	if r == nil {
		return nil
	}
	out := &models.AppReview{
		Version:    r.Version,
		Generate:   reviewGenerateToModel(r.Generate),
		Parameters: r.Parameters,
	}
	if len(r.Inputs) > 0 {
		out.Inputs = make(map[string]models.ReviewInput, len(r.Inputs))
		for name, in := range r.Inputs {
			out.Inputs[name] = models.ReviewInput{
				Generate: reviewGenerateToModel(in.Generate),
				Files:    in.Files,
				Dataset:  reviewDatasetToModel(in.Dataset),
			}
		}
	}
	return out
}

func reviewGenerateToModel(g *store_dynamodb.ReviewGenerate) *models.ReviewGenerate {
	if g == nil {
		return nil
	}
	return &models.ReviewGenerate{Image: g.Image, Command: g.Command, Root: g.Root, Source: reviewDatasetToModel(g.Source)}
}

func reviewDatasetToModel(d *store_dynamodb.ReviewDataset) *models.ReviewDataset {
	if d == nil {
		return nil
	}
	return &models.ReviewDataset{DOI: d.DOI, URL: d.URL, Files: d.Files}
}

func AppCitationToModel(c *store_dynamodb.AppCitation) *models.AppCitation {
	if c == nil {
		return nil
	}
	out := &models.AppCitation{
		Title:        c.Title,
		Version:      c.Version,
		DOI:          c.DOI,
		DateReleased: c.DateReleased,
		Authors:      []models.CitationAuthor{},
		RelatedWorks: []models.RelatedWork{},
	}
	for _, a := range c.Authors {
		out.Authors = append(out.Authors, models.CitationAuthor{Name: a.Name, GivenNames: a.GivenNames,
			FamilyNames: a.FamilyNames, Orcid: a.Orcid, Affiliation: a.Affiliation})
	}
	if c.PreferredCitation != nil {
		p := relatedWorkToModel(*c.PreferredCitation)
		out.PreferredCitation = &p
	}
	for _, w := range c.RelatedWorks {
		out.RelatedWorks = append(out.RelatedWorks, relatedWorkToModel(w))
	}
	return out
}

// appStoreVariantsToModels lists a version's variants in app.yml order
// (variantOrder), then any the order doesn't name, by id. The default
// variant's status falls back to the version's, which is its status; another
// variant without a status has no build recorded and has none.
func appStoreVariantsToModels(v store_dynamodb.AppStoreVersion) []models.AppStoreVariant {
	if len(v.Variants) == 0 {
		return nil
	}
	ids := make([]string, 0, len(v.Variants))
	seen := map[string]bool{}
	for _, id := range v.VariantOrder {
		if _, ok := v.Variants[id]; ok && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	var rest []string
	for id := range v.Variants {
		if !seen[id] {
			rest = append(rest, id)
		}
	}
	sort.Strings(rest)
	ids = append(ids, rest...)

	out := make([]models.AppStoreVariant, 0, len(ids))
	for _, id := range ids {
		sv := v.Variants[id]
		isDefault := id == v.DefaultVariant
		status := sv.Status
		if status == "" && isDefault {
			status = v.Status
		}
		out = append(out, models.AppStoreVariant{
			ID:            id,
			Name:          sv.Name,
			Description:   sv.Description,
			Default:       isDefault,
			Status:        status,
			FailureReason: sv.FailureReason,
			Manifest:      AppManifestToModel(sv.Manifest),
			ImageDigest:   sv.ImageDigest,
		})
	}
	return out
}

func relatedWorkToModel(w store_dynamodb.RelatedWork) models.RelatedWork {
	return models.RelatedWork{DOI: w.DOI, RelationType: w.RelationType, Title: w.Title, Authors: w.Authors,
		Year: w.Year, Journal: w.Journal, URL: w.URL, Source: w.Source}
}

func AppManifestToModel(m *store_dynamodb.AppManifest) *models.AppManifest {
	if m == nil {
		return nil
	}
	params := AppParametersToModels(m.Parameters)
	if params == nil {
		params = []models.AppParameter{}
	}
	return &models.AppManifest{
		Name:        m.Name,
		Description: m.Description,
		Tags:        m.Tags,
		Maintainers: appMaintainersToModels(m.Maintainers),
		Runtime: models.AppRuntime{
			CPU:            m.Runtime.CPU,
			Memory:         m.Runtime.Memory,
			ComputeTypes:   m.Runtime.ComputeTypes,
			TimeoutSeconds: m.Runtime.TimeoutSeconds,
			LLM:            m.Runtime.LLM,
			Interactive:    m.Runtime.Interactive,
			SessionType:    m.Runtime.SessionType,
			Kernel:         m.Runtime.Kernel,
		},
		Parameters: params,
		Inputs:     appPortsToModels(m.Inputs),
		Outputs:    appPortsToModels(m.Outputs),
		Variant:    appManifestVariantToModel(m.Variant),
		References: appReferencesToModels(m.References),
	}
}

func appReferencesToModels(refs []store_dynamodb.AppReference) []models.AppReference {
	if len(refs) == 0 {
		return nil
	}
	annotation := func(a *store_dynamodb.ReferenceAnnotation) *models.ReferenceAnnotation {
		if a == nil {
			return nil
		}
		return &models.ReferenceAnnotation{Provider: a.Provider, Version: a.Version}
	}
	out := make([]models.AppReference, 0, len(refs))
	for _, r := range refs {
		m := models.AppReference{
			Name: r.Name, Description: r.Description, Kind: r.Kind, AssetClass: r.AssetClass, ToolVersion: r.ToolVersion,
			Choice: r.Choice, TaxonomyID: r.TaxonomyID, Assembly: r.Assembly, Seqcol: r.Seqcol, Annotation: annotation(r.Annotation),
			Organisms: r.Organisms, Source: r.Source, SHA256: r.SHA256, License: r.License,
		}
		if r.Default != nil {
			m.Default = &models.ReferenceIdentity{TaxonomyID: r.Default.TaxonomyID, Assembly: r.Default.Assembly,
				Seqcol: r.Default.Seqcol, Annotation: annotation(r.Default.Annotation)}
		}
		out = append(out, m)
	}
	return out
}

func appManifestVariantToModel(v *store_dynamodb.AppManifestVariant) *models.AppManifestVariant {
	if v == nil {
		return nil
	}
	return &models.AppManifestVariant{ID: v.ID, Name: v.Name, Description: v.Description}
}

func appPortsToModels(ports []store_dynamodb.AppPort) []models.AppPort {
	out := []models.AppPort{}
	for _, p := range ports {
		media := p.MediaTypes
		if media == nil {
			media = []string{}
		}
		out = append(out, models.AppPort{Name: p.Name, Description: p.Description, MediaTypes: media, Formats: p.Formats,
			Container: p.Container, ConformsTo: p.ConformsTo, Placement: p.Placement})
	}
	return out
}

func appMaintainersToModels(ms []store_dynamodb.AppMaintainer) []models.AppMaintainer {
	if len(ms) == 0 {
		return nil
	}
	out := make([]models.AppMaintainer, 0, len(ms))
	for _, m := range ms {
		out = append(out, models.AppMaintainer{Name: m.Name, Email: m.Email, Orcid: m.Orcid})
	}
	return out
}

func AppStoreVersionsToModels(versions []store_dynamodb.AppStoreVersion) []models.AppStoreVersion {
	result := []models.AppStoreVersion{}
	for _, v := range versions {
		result = append(result, AppStoreVersionToModel(v))
	}
	return result
}

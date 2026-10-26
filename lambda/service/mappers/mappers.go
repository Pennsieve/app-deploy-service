package mappers

import (
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
	}
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
			Interactive:    m.Runtime.Interactive,
			SessionType:    m.Runtime.SessionType,
			Kernel:         m.Runtime.Kernel,
		},
		Parameters: params,
		Inputs:     appPortsToModels(m.Inputs),
		Outputs:    appPortsToModels(m.Outputs),
	}
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

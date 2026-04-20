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
	return models.AppStoreApplication{
		Uuid:       a.Uuid,
		SourceUrl:  a.SourceUrl,
		SourceType: a.SourceType,
		IsPrivate:  a.IsPrivate,
		Visibility: a.Visibility,
		OwnerId:    a.OwnerId,
		CreatedAt:  a.CreatedAt,
	}
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
		Uuid:          v.Uuid,
		ApplicationId: v.ApplicationId,
		Version:       v.Version,
		ReleaseId:     v.ReleaseId,
		CreatedAt:     v.CreatedAt,
		Status:        v.Status,
	}
}

func AppStoreVersionsToModels(versions []store_dynamodb.AppStoreVersion) []models.AppStoreVersion {
	result := []models.AppStoreVersion{}
	for _, v := range versions {
		result = append(result, AppStoreVersionToModel(v))
	}
	return result
}

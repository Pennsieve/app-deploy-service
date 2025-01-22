package mappers

import (
	"github.com/pennsieve/app-deploy-service/service/models"
	"github.com/pennsieve/app-deploy-service/service/store_dynamodb"
)

func StoreToModel(a store_dynamodb.Application) models.Application {
	return models.Application{
		Uuid:                     a.Uuid,
		ApplicationId:            a.ApplicationId,
		ApplicationContainerName: a.ApplicationContainerName,
		Name:                     a.Name,
		Description:              a.Description,
		Resources: models.ApplicationResources{
			CPU:    a.CPU,
			Memory: a.Memory,
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

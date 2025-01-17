package mappers

import (
	"github.com/pennsieve/app-deploy-service/service/models"
	"github.com/pennsieve/app-deploy-service/service/store_dynamodb"
)

func DynamoDBApplicationToJsonApplication(dynamoApplication store_dynamodb.Application) models.Application {
	return models.Application{
		Uuid:                     dynamoApplication.Uuid,
		ApplicationId:            dynamoApplication.ApplicationId,
		ApplicationContainerName: dynamoApplication.ApplicationContainerName,
		Name:                     dynamoApplication.Name,
		Description:              dynamoApplication.Description,
		Resources: models.ApplicationResources{
			CPU:    dynamoApplication.CPU,
			Memory: dynamoApplication.Memory,
		},
		ApplicationType: dynamoApplication.ApplicationType,
		Account: models.Account{
			Uuid:        dynamoApplication.AccountUuid,
			AccountId:   dynamoApplication.AccountId,
			AccountType: dynamoApplication.AccountType,
		},
		ComputeNode: models.ComputeNode{
			Uuid:  dynamoApplication.ComputeNodeUuid,
			EfsId: dynamoApplication.ComputeNodeEfsId,
		},
		Source: models.Source{
			SourceType: dynamoApplication.SourceType,
			Url:        dynamoApplication.SourceUrl,
		},
		Destination: models.Destination{
			DestinationType: dynamoApplication.DestinationType,
			Url:             dynamoApplication.DestinationUrl,
		},
		Params:           dynamoApplication.Params,
		CommandArguments: dynamoApplication.CommandArguments,
		Env:              dynamoApplication.Env,
		CreatedAt:        dynamoApplication.CreatedAt,
		OrganizationId:   dynamoApplication.OrganizationId,
		UserId:           dynamoApplication.UserId,
		Status:           dynamoApplication.Status,
	}
}
func DynamoDBApplicationsToJsonApplications(dynamoApplications []store_dynamodb.Application) []models.Application {
	applications := []models.Application{}

	for _, a := range dynamoApplications {
		applications = append(applications, DynamoDBApplicationToJsonApplication(a))
	}

	return applications
}

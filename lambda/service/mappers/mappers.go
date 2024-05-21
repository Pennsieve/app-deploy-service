package mappers

import (
	"github.com/pennsieve/app-deploy-service/service/models"
	"github.com/pennsieve/app-deploy-service/service/store_dynamodb"
)

func DynamoDBApplicationToJsonApplication(dynamoApplications []store_dynamodb.Application) []models.Application {
	applications := []models.Application{}

	for _, a := range dynamoApplications {
		applications = append(applications, models.Application{
			Uuid:            a.Uuid,
			ApplicationId:   a.ApplicationId,
			Name:            a.Name,
			Description:     a.Description,
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
			Env:            a.Env,
			CreatedAt:      a.CreatedAt,
			OrganizationId: a.OrganizationId,
			UserId:         a.UserId,
		})
	}

	return applications
}

package mappers

import (
	"github.com/pennsieve/app-deploy-service/service/models"
	"github.com/pennsieve/app-deploy-service/service/store_dynamodb"
)

func DynamoDBNodeToJsonNode(dynamoNodes []store_dynamodb.App) []models.Application {
	applications := []models.Application{}

	for _, c := range dynamoNodes {
		applications = append(applications, models.Application{
			Uuid:           c.Uuid,
			AppEcrUrl:      c.AppEcrUrl,
			CreatedAt:      c.CreatedAt,
			OrganizationId: c.OrganizationId,
			UserId:         c.UserId,
		})
	}

	return applications
}

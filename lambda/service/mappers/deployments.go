package mappers

import (
	"github.com/pennsieve/app-deploy-service/service/models"
	"github.com/pennsieve/app-deploy-service/service/store_dynamodb"
)

func DeploymentItemToModel(item store_dynamodb.Deployment) models.Deployment {
	return models.Deployment{
		DeploymentId:  item.DeploymentId,
		ApplicationId: item.ApplicationId,
		InitiatedAt:   item.InitiatedAt,
		Version:       item.Version,
		LastStatus:    item.LastStatus,
		DesiredStatus: item.DesiredStatus,
		TaskArn:       item.TaskArn,
		UpdatedAt:     item.UpdatedAt,
		CreatedAt:     item.CreatedAt,
		StartedAt:     item.StartedAt,
		StoppedAt:     item.StoppedAt,
		StopCode:      item.StopCode,
		StoppedReason: item.StoppedReason,
		Errored:       item.Errored,
	}
}

func DeploymentItemsToModels(items []store_dynamodb.Deployment) []models.Deployment {
	var ms []models.Deployment
	for _, item := range items {
		ms = append(ms, DeploymentItemToModel(item))
	}
	return ms
}

package handler

import "github.com/pennsieve/app-deploy-service/status/models"

type DeploymentUpdateConflict struct {
	Existing           *models.Deployment
	UnmarshallingError error
}

func (c *DeploymentUpdateConflict) Error() string {
	return "conflicting deployment exists"
}

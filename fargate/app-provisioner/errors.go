package main

import (
	"context"
	"fmt"
	"github.com/pennsieve/app-deploy-service/app-provisioner/provisioner/store_dynamodb"
	"log"
)

type ErrorHandler struct {
	ApplicationsStore store_dynamodb.DynamoDBStore
	DeploymentsStore  *store_dynamodb.DeploymentsStore
	ApplicationId     string
	DeploymentId      string
}

func NewErrorHandler(applicationsStore store_dynamodb.DynamoDBStore, deploymentsStore *store_dynamodb.DeploymentsStore, applicationId string, deploymentId string) *ErrorHandler {
	return &ErrorHandler{ApplicationsStore: applicationsStore, DeploymentsStore: deploymentsStore, ApplicationId: applicationId, DeploymentId: deploymentId}
}

func (h *ErrorHandler) handleError(ctx context.Context, err error) {
	msg := fmt.Sprintf("error: %s", err.Error())
	if appStoreErr := h.ApplicationsStore.UpdateStatus(ctx, msg, h.ApplicationId); appStoreErr != nil {
		log.Printf("warning: error updating applications table with error: %s: %s\n", msg, appStoreErr.Error())
	}
	if deployStoreErr := h.DeploymentsStore.SetErrored(ctx, h.DeploymentId); deployStoreErr != nil {
		log.Printf("warning: error setting errored on deployments table: %s\n", deployStoreErr.Error())
	}
}

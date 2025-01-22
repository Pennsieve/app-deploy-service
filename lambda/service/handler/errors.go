package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/pennsieve/app-deploy-service/service/store_dynamodb"
	"log"

	"github.com/pennsieve/app-deploy-service/service/models"
)

var ErrUnmarshaling = errors.New("error unmarshaling body")
var ErrUnsupportedPath = errors.New("unsupported path")
var ErrUnsupportedRoute = errors.New("unsupported route")
var ErrRunningFargateTask = errors.New("error running fargate task")
var ErrConfig = errors.New("error loading AWS config")
var ErrNoRecordsFound = errors.New("error no records found")
var ErrRecordExists = errors.New("error record already exists")
var ErrMarshaling = errors.New("error marshaling item")
var ErrDynamoDB = errors.New("error performing action on DynamoDB table")
var ErrNotPermitted = errors.New("not permitted")
var ErrStoringApplication = errors.New("error storing application")
var ErrStoringDeployment = errors.New("error storing deployment")

func handlerError(handlerName string, errorMessage error) string {
	log.Printf("%s: %s", handlerName, errorMessage.Error())
	m, err := json.Marshal(models.ApplicationResponse{
		Message: errorMessage.Error(),
	})
	if err != nil {
		log.Printf("%s: error marshalling error message %s: %s", handlerName, errorMessage.Error(), err.Error())
		return err.Error()
	}

	return string(m)
}

type ErrorHandler struct {
	HandlerName       string
	ApplicationsStore store_dynamodb.DynamoDBStore
	DeploymentsStore  *store_dynamodb.DeploymentsStore
	ApplicationId     string
	DeploymentId      string
}

func NewErrorHandler(handlerName string, applicationsStore store_dynamodb.DynamoDBStore, deploymentsStore *store_dynamodb.DeploymentsStore, applicationId string, deploymentId string) *ErrorHandler {
	return &ErrorHandler{HandlerName: handlerName, ApplicationsStore: applicationsStore, DeploymentsStore: deploymentsStore, ApplicationId: applicationId, DeploymentId: deploymentId}
}

func (h *ErrorHandler) handleError(ctx context.Context, err error) string {
	msg := fmt.Sprintf("error: %s", err.Error())
	if appStoreErr := h.ApplicationsStore.UpdateStatus(ctx, msg, h.ApplicationId); appStoreErr != nil {
		log.Printf("warning: error updating applications table with error: %s: %s\n", msg, appStoreErr.Error())
	}
	if deployStoreErr := h.DeploymentsStore.SetErrored(ctx, h.DeploymentId); deployStoreErr != nil {
		log.Printf("warning: error setting errored on deployments table: %s\n", deployStoreErr.Error())
	}
	return handlerError(h.HandlerName, err)
}

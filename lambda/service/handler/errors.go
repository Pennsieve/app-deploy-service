package handler

import (
	"encoding/json"
	"errors"
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

func handlerError(handlerName string, errorMessage error) string {
	log.Printf("%s: %s", handlerName, errorMessage.Error())
	m, err := json.Marshal(models.ApplicationResponse{
		Message: errorMessage.Error(),
	})
	if err != nil {
		log.Printf("%s: %s", handlerName, err.Error())
		return err.Error()
	}

	return string(m)
}

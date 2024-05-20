package handler

import (
	"errors"
	"fmt"
)

var ErrUnmarshaling = errors.New("error unmarshaling body")
var ErrUnsupportedPath = errors.New("unsupported path")
var ErrUnsupportedRoute = errors.New("unsupported route")
var ErrRunningFargateTask = errors.New("error running Rehydrate fargate task")
var ErrConfig = errors.New("error loading AWS config")
var ErrNoRecordsFound = errors.New("error no records found")
var ErrMarshaling = errors.New("error marshaling item")
var ErrDynamoDB = errors.New("error performing action on DynamoDB table")

func handlerError(handlerName string, handlerError error) string {
	return fmt.Sprintf("%s: %s", handlerName, handlerError.Error())
}

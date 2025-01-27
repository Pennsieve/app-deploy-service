package models

import (
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/pennsieve/app-deploy-service/status/dydbutils"
)

// These *Field const must match the field names in the Applications table

const ApplicationKeyField = "uuid"
const ApplicationStatusField = "registrationStatus"

func ApplicationKey(applicationId string) map[string]types.AttributeValue {
	return map[string]types.AttributeValue{ApplicationKeyField: dydbutils.StringAttributeValue(applicationId)}
}

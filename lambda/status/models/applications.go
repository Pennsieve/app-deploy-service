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

// An App Store version's variants (a map keyed by variant id) and the
// fields of a variant the status lambda sets when its build ends.
const VariantsField = "variants"
const VariantStatusField = "status"
const VariantFailureReasonField = "failureReason"

// What the status lambda records about an App Store version's image: the
// version's destinationUrl (its image, which the provisioner sets) and the
// digest of what was pushed there, on the version and on a variant.
const ApplicationDestinationUrlField = "destinationUrl"
const ImageDigestField = "imageDigest"

package store_dynamodb

import (
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

type Application struct {
	Uuid                     string `dynamodbav:"uuid"`
	Name                     string `dynamodbav:"name"`
	Description              string `dynamodbav:"description"`
	ApplicationType          string `dynamodbav:"applicationType"`
	ApplicationId            string `dynamodbav:"applicationId"`
	ApplicationContainerName string `dynamodbav:"applicationContainerName"`

	AccountUuid string `dynamodbav:"accountUuid"`
	AccountId   string `dynamodbav:"accountId"`
	AccountType string `dynamodbav:"accountType"`

	ComputeNodeUuid  string `dynamodbav:"computeNodeUuid"`
	ComputeNodeEfsId string `dynamodbav:"computeNodeEfsId"`

	SourceType string `dynamodbav:"sourceType"`
	SourceUrl  string `dynamodbav:"sourceUrl"`

	DestinationType string `dynamodbav:"destinationType"`
	DestinationUrl  string `dynamodbav:"destinationUrl"`

	CPU          int      `dynamodbav:"cpu"`
	Memory       int      `dynamodbav:"memory"`
	RunOnGPU     bool     `dynamodbav:"runOnGpu"`
	ComputeTypes []string `dynamodbav:"computeTypes,omitempty"`

	Env string `dynamodbav:"environment"`

	OrganizationId string `dynamodbav:"organizationId"`
	UserId         string `dynamodbav:"userId"`
	CreatedAt      string `dynamodbav:"createdAt"`

	Params           interface{} `dynamodbav:"params"`
	CommandArguments interface{} `dynamodbav:"commandArguments"`

	Status string `dynamodbav:"registrationStatus"`
}

type ApplicationKey struct {
	Uuid string `dynamodbav:"uuid"`
}

func (i Application) GetKey() map[string]types.AttributeValue {
	uuid, err := attributevalue.Marshal(i.Uuid)
	if err != nil {
		panic(err)
	}

	return map[string]types.AttributeValue{"uuid": uuid}
}

// AppStoreApplication represents an application in the appstore.
// One record per unique sourceUrl (the git repository).
type AppStoreApplication struct {
	Uuid       string `dynamodbav:"uuid"`
	SourceUrl  string `dynamodbav:"sourceUrl"`
	SourceType string `dynamodbav:"sourceType"`
	IsPrivate  bool   `dynamodbav:"isPrivate"`
	CreatedAt  string `dynamodbav:"createdAt"`
}

func (i AppStoreApplication) GetKey() map[string]types.AttributeValue {
	uuid, err := attributevalue.Marshal(i.Uuid)
	if err != nil {
		panic(err)
	}

	return map[string]types.AttributeValue{"uuid": uuid}
}

// AppStoreVersion represents a specific version of an appstore application.
// Multiple versions can exist for a single application (sourceUrl).
type AppStoreVersion struct {
	Uuid           string `dynamodbav:"uuid"`
	ApplicationId  string `dynamodbav:"applicationId"`
	Version        string `dynamodbav:"version"`
	ReleaseId      int    `dynamodbav:"releaseId"`
	DestinationUrl string `dynamodbav:"destinationUrl"`
	CreatedAt      string `dynamodbav:"createdAt"`
	Status         string `dynamodbav:"registrationStatus"`
}

func (i AppStoreVersion) GetKey() map[string]types.AttributeValue {
	uuid, err := attributevalue.Marshal(i.Uuid)
	if err != nil {
		panic(err)
	}

	return map[string]types.AttributeValue{"uuid": uuid}
}

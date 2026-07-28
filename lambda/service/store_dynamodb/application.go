package store_dynamodb

import (
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/pennsieve/app-deploy-service/service/models"
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
	Uuid       string                `dynamodbav:"uuid"`
	SourceUrl  string                `dynamodbav:"sourceUrl"`
	SourceType string                `dynamodbav:"sourceType"`
	IsPrivate  bool                  `dynamodbav:"isPrivate"`
	Visibility string                `dynamodbav:"visibility"`
	OwnerId    string                `dynamodbav:"ownerId"`
	CreatedAt  string                `dynamodbav:"createdAt"`
	Status     models.AppStoreStatus `dynamodbav:"status,omitempty"`
	// Params are the processor parameter declarations parsed from the app's
	// app.yml at publish time. app.yml is the source of truth; downstream
	// services (e.g. workflow-service) read these as parameter defaults.
	Params []AppParameter `dynamodbav:"params,omitempty"`
}

// AppParameter is a single processor parameter declaration from app.yml. A
// parameter with no DefaultValue is treated as required by consumers. The
// attribute names must stay stable — consumers read them by these keys.
type AppParameter struct {
	Name         string   `dynamodbav:"name"`
	Type         string   `dynamodbav:"type,omitempty"`
	Description  string   `dynamodbav:"description,omitempty"`
	DefaultValue string   `dynamodbav:"defaultValue,omitempty"`
	ValidValues  []string `dynamodbav:"validValues,omitempty"`
}

type AppAccess struct {
	EntityId       string `dynamodbav:"entityId"`
	AppId          string `dynamodbav:"appId"`
	EntityType     string `dynamodbav:"entityType"`
	EntityRawId    string `dynamodbav:"entityRawId"`
	AppUuid        string `dynamodbav:"appUuid"`
	AccessType     string `dynamodbav:"accessType"`
	OrganizationId string `dynamodbav:"organizationId,omitempty"`
	GrantedAt      string `dynamodbav:"grantedAt"`
	GrantedBy      string `dynamodbav:"grantedBy"`
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

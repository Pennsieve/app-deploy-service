package store_dynamodb

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// AppStoreVersionDBStore operates on the appstore versions table.
type AppStoreVersionDBStore interface {
	UpdateStatus(ctx context.Context, newStatus string, uuid string) error
	UpdateDestinationUrl(ctx context.Context, uuid string, destinationUrl string, status string) error
	// GetBuilds reads what the version's images are built from: its
	// variants and build settings (READ_VERSION_BUILDS).
	GetBuilds(ctx context.Context, uuid string) (VersionBuilds, error)
	// UpdateVariant records a variant's build: its image (if any), its
	// status and, for an error, why.
	UpdateVariant(ctx context.Context, uuid string, variant string, update VariantUpdate) error
}

// VersionBuilds is what a version's images are built from (written by the
// service lambda at registration, docs/app-manifest-variants.md).
type VersionBuilds struct {
	DefaultVariant string                  `dynamodbav:"defaultVariant,omitempty"`
	VariantOrder   []string                `dynamodbav:"variantOrder,omitempty"`
	Variants       map[string]VariantBuild `dynamodbav:"variants,omitempty"`
	// Build is the top-level build settings of a version without variants.
	Build *BuildSettings `dynamodbav:"build,omitempty"`
}

// VariantBuild is the part of a stored variant its build needs.
type VariantBuild struct {
	Build           *BuildSettings `dynamodbav:"build,omitempty"`
	DeploymentId    string         `dynamodbav:"deploymentId,omitempty"`
	BuildStorageGiB int32          `dynamodbav:"buildStorageGiB,omitempty"`
}

// BuildSettings is app.yml's build: Docker build args, the target stage and
// the Dockerfile's path.
type BuildSettings struct {
	Args       map[string]string `dynamodbav:"args,omitempty"`
	Target     string            `dynamodbav:"target,omitempty"`
	Dockerfile string            `dynamodbav:"dockerfile,omitempty"`
}

// VariantUpdate is a change to one variant's build record; empty fields are
// left as they are.
type VariantUpdate struct {
	DestinationUrl string
	Status         string
	FailureReason  string
}

type AppStoreVersionDatabaseStore struct {
	DB        *dynamodb.Client
	TableName string
}

func NewAppStoreVersionDatabaseStore(db *dynamodb.Client, tableName string) *AppStoreVersionDatabaseStore {
	return &AppStoreVersionDatabaseStore{db, tableName}
}

func (r *AppStoreVersionDatabaseStore) UpdateStatus(ctx context.Context, newStatus string, uuid string) error {
	key, err := attributevalue.MarshalMap(ApplicationKey{Uuid: uuid})
	if err != nil {
		return fmt.Errorf("error marshaling key for version status update: %w", err)
	}

	_, err = r.DB.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName: aws.String(r.TableName),
		Key:       key,
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":s": &types.AttributeValueMemberS{Value: newStatus},
		},
		UpdateExpression: aws.String("set registrationStatus = :s"),
	})
	if err != nil {
		return fmt.Errorf("error updating appstore version status: %w", err)
	}

	return nil
}

func (r *AppStoreVersionDatabaseStore) UpdateDestinationUrl(ctx context.Context, uuid string, destinationUrl string, status string) error {
	key, err := attributevalue.MarshalMap(ApplicationKey{Uuid: uuid})
	if err != nil {
		return fmt.Errorf("error marshaling key for version destination update: %w", err)
	}

	_, err = r.DB.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName: aws.String(r.TableName),
		Key:       key,
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":d": &types.AttributeValueMemberS{Value: destinationUrl},
			":s": &types.AttributeValueMemberS{Value: status},
		},
		UpdateExpression: aws.String("set destinationUrl = :d, registrationStatus = :s"),
	})
	if err != nil {
		return fmt.Errorf("error updating appstore version destination URL: %w", err)
	}

	return nil
}

func (r *AppStoreVersionDatabaseStore) GetBuilds(ctx context.Context, uuid string) (VersionBuilds, error) {
	var out VersionBuilds
	key, err := attributevalue.MarshalMap(ApplicationKey{Uuid: uuid})
	if err != nil {
		return out, fmt.Errorf("error marshaling key for version builds: %w", err)
	}
	res, err := r.DB.GetItem(ctx, &dynamodb.GetItemInput{
		TableName:                aws.String(r.TableName),
		Key:                      key,
		ConsistentRead:           aws.Bool(true),
		ProjectionExpression:     aws.String("#d, #o, #v, #b"),
		ExpressionAttributeNames: map[string]string{"#d": "defaultVariant", "#o": "variantOrder", "#v": "variants", "#b": "build"},
	})
	if err != nil {
		return out, fmt.Errorf("error reading the builds of version %s: %w", uuid, err)
	}
	if res.Item == nil {
		return out, fmt.Errorf("version %s not found", uuid)
	}
	if err := attributevalue.UnmarshalMap(res.Item, &out); err != nil {
		return out, fmt.Errorf("error reading the builds of version %s: %w", uuid, err)
	}
	return out, nil
}

func (r *AppStoreVersionDatabaseStore) UpdateVariant(ctx context.Context, uuid string, variant string, update VariantUpdate) error {
	in, err := variantUpdateInput(r.TableName, uuid, variant, update)
	if err != nil {
		return err
	}
	if in == nil {
		return nil
	}
	if _, err := r.DB.UpdateItem(ctx, in); err != nil {
		return fmt.Errorf("error updating variant %s of version %s: %w", variant, uuid, err)
	}
	return nil
}

// variantUpdateInput sets the given fields of variants.<variant>. The
// variant must already be recorded (a missing one is a conditional check
// failure, never a new partial variant). nil when there's nothing to set.
func variantUpdateInput(table, uuid, variant string, u VariantUpdate) (*dynamodb.UpdateItemInput, error) {
	key, err := attributevalue.MarshalMap(ApplicationKey{Uuid: uuid})
	if err != nil {
		return nil, fmt.Errorf("error marshaling key for variant update: %w", err)
	}
	names := map[string]string{"#vs": "variants", "#id": variant}
	values := map[string]types.AttributeValue{}
	var sets []string
	set := func(field, value string) {
		if value == "" {
			return
		}
		n, v := "#"+field, ":"+field
		names[n] = field
		values[v] = &types.AttributeValueMemberS{Value: value}
		sets = append(sets, "#vs.#id."+n+" = "+v)
	}
	set("destinationUrl", u.DestinationUrl)
	set("status", u.Status)
	set("failureReason", u.FailureReason)
	if len(sets) == 0 {
		return nil, nil
	}
	return &dynamodb.UpdateItemInput{
		TableName:                 aws.String(table),
		Key:                       key,
		UpdateExpression:          aws.String("SET " + strings.Join(sets, ", ")),
		ConditionExpression:       aws.String("attribute_exists(#vs.#id)"),
		ExpressionAttributeNames:  names,
		ExpressionAttributeValues: values,
	}, nil
}

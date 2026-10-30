package store_dynamodb

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/expression"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// AppStoreVersionTableAPI is a narrow interface containing only the DynamoDB client methods used by AppStoreVersionDatabaseStore.
type AppStoreVersionTableAPI interface {
	PutItem(ctx context.Context, params *dynamodb.PutItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error)
	Query(ctx context.Context, params *dynamodb.QueryInput, optFns ...func(*dynamodb.Options)) (*dynamodb.QueryOutput, error)
	GetItem(ctx context.Context, params *dynamodb.GetItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error)
	UpdateItem(ctx context.Context, params *dynamodb.UpdateItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.UpdateItemOutput, error)
}

// AppStoreVersionDBStore operates on the appstore versions table.
type AppStoreVersionDBStore interface {
	GetById(context.Context, string) (*AppStoreVersion, error)
	GetByApplicationId(context.Context, string) ([]AppStoreVersion, error)
	GetByApplicationIdAndVersion(ctx context.Context, applicationId string, version string) ([]AppStoreVersion, error)
	Insert(context.Context, AppStoreVersion) error
	UpdateStatus(ctx context.Context, newStatus string, uuid string) error
	SetManifest(ctx context.Context, uuid string, manifest *AppManifest) error
	SetRegistrationDetails(ctx context.Context, uuid string, license string, manifestProblems []string) error
	SetVariants(ctx context.Context, uuid string, variants VersionVariants) error
}

type AppStoreVersionDatabaseStore struct {
	api       AppStoreVersionTableAPI
	TableName string
}

func NewAppStoreVersionDatabaseStore(api AppStoreVersionTableAPI, tableName string) *AppStoreVersionDatabaseStore {
	return &AppStoreVersionDatabaseStore{api, tableName}
}

// GetById returns the version with the given uuid, or nil, nil when there is none.
func (r *AppStoreVersionDatabaseStore) GetById(ctx context.Context, uuid string) (*AppStoreVersion, error) {
	response, err := r.api.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(r.TableName),
		Key:       AppStoreVersion{Uuid: uuid}.GetKey(),
	})
	if err != nil {
		return nil, fmt.Errorf("error getting appstore version: %w", err)
	}
	if response.Item == nil {
		return nil, nil
	}
	var version AppStoreVersion
	if err := attributevalue.UnmarshalMap(response.Item, &version); err != nil {
		return nil, fmt.Errorf("error unmarshaling appstore version: %w", err)
	}
	return &version, nil
}

// GetByApplicationId returns all versions for a given application using the applicationId-version-index GSI.
func (r *AppStoreVersionDatabaseStore) GetByApplicationId(ctx context.Context, applicationId string) ([]AppStoreVersion, error) {
	versions := []AppStoreVersion{}

	keyCondition := expression.Key("applicationId").Equal(expression.Value(applicationId))
	expr, err := expression.NewBuilder().WithKeyCondition(keyCondition).Build()
	if err != nil {
		return versions, fmt.Errorf("error building expression: %w", err)
	}

	response, err := r.api.Query(ctx, &dynamodb.QueryInput{
		TableName:                 aws.String(r.TableName),
		IndexName:                 aws.String("applicationId-version-index"),
		ExpressionAttributeNames:  expr.Names(),
		ExpressionAttributeValues: expr.Values(),
		KeyConditionExpression:    expr.KeyCondition(),
	})
	if err != nil {
		return versions, fmt.Errorf("error querying appstore versions by applicationId: %w", err)
	}

	err = attributevalue.UnmarshalListOfMaps(response.Items, &versions)
	if err != nil {
		return versions, fmt.Errorf("error unmarshaling appstore versions: %w", err)
	}

	return versions, nil
}

// GetByApplicationIdAndVersion returns a specific version using the applicationId-version-index GSI.
func (r *AppStoreVersionDatabaseStore) GetByApplicationIdAndVersion(ctx context.Context, applicationId string, version string) ([]AppStoreVersion, error) {
	versions := []AppStoreVersion{}

	keyCondition := expression.Key("applicationId").Equal(expression.Value(applicationId)).
		And(expression.Key("version").Equal(expression.Value(version)))
	expr, err := expression.NewBuilder().WithKeyCondition(keyCondition).Build()
	if err != nil {
		return versions, fmt.Errorf("error building expression: %w", err)
	}

	response, err := r.api.Query(ctx, &dynamodb.QueryInput{
		TableName:                 aws.String(r.TableName),
		IndexName:                 aws.String("applicationId-version-index"),
		ExpressionAttributeNames:  expr.Names(),
		ExpressionAttributeValues: expr.Values(),
		KeyConditionExpression:    expr.KeyCondition(),
	})
	if err != nil {
		return versions, fmt.Errorf("error querying appstore version: %w", err)
	}

	err = attributevalue.UnmarshalListOfMaps(response.Items, &versions)
	if err != nil {
		return versions, fmt.Errorf("error unmarshaling appstore version: %w", err)
	}

	return versions, nil
}

func (r *AppStoreVersionDatabaseStore) Insert(ctx context.Context, version AppStoreVersion) error {
	item, err := attributevalue.MarshalMap(version)
	if err != nil {
		return fmt.Errorf("error marshaling appstore version: %w", err)
	}
	_, err = r.api.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(r.TableName), Item: item,
	})
	if err != nil {
		return fmt.Errorf("error inserting appstore version: %w", err)
	}

	return nil
}

func (r *AppStoreVersionDatabaseStore) UpdateStatus(ctx context.Context, newStatus string, uuid string) error {
	key, err := attributevalue.MarshalMap(ApplicationKey{Uuid: uuid})
	if err != nil {
		return fmt.Errorf("error marshaling key for appstore version status update: %w", err)
	}

	_, err = r.api.UpdateItem(ctx, &dynamodb.UpdateItemInput{
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

// SetRegistrationDetails records what registration learned about a version
// besides its manifest: the repository's license and how its app.yml doesn't
// follow the schema. Empty values remove the attributes.
func (r *AppStoreVersionDatabaseStore) SetRegistrationDetails(ctx context.Context, uuid string, license string, manifestProblems []string) error {
	key, err := attributevalue.MarshalMap(ApplicationKey{Uuid: uuid})
	if err != nil {
		return fmt.Errorf("error marshaling key for appstore version details: %w", err)
	}
	values := map[string]types.AttributeValue{}
	var set, remove []string
	if license != "" {
		values[":l"] = &types.AttributeValueMemberS{Value: license}
		set = append(set, "license = :l")
	} else {
		remove = append(remove, "license")
	}
	if len(manifestProblems) > 0 {
		v, err := attributevalue.Marshal(manifestProblems)
		if err != nil {
			return fmt.Errorf("error marshaling manifest problems: %w", err)
		}
		values[":p"] = v
		set = append(set, "manifestProblems = :p")
	} else {
		remove = append(remove, "manifestProblems")
	}
	expr := ""
	if len(set) > 0 {
		expr = "set " + strings.Join(set, ", ")
	}
	if len(remove) > 0 {
		expr = strings.TrimSpace(expr + " remove " + strings.Join(remove, ", "))
	}
	in := &dynamodb.UpdateItemInput{
		TableName:        aws.String(r.TableName),
		Key:              key,
		UpdateExpression: aws.String(expr),
	}
	if len(values) > 0 {
		in.ExpressionAttributeValues = values
	}
	if _, err := r.api.UpdateItem(ctx, in); err != nil {
		return fmt.Errorf("error setting appstore version details: %w", err)
	}
	return nil
}

// SetManifest records a version's parsed app.yml.
func (r *AppStoreVersionDatabaseStore) SetManifest(ctx context.Context, uuid string, manifest *AppManifest) error {
	key, err := attributevalue.MarshalMap(ApplicationKey{Uuid: uuid})
	if err != nil {
		return fmt.Errorf("error marshaling key for appstore version manifest: %w", err)
	}
	value, err := attributevalue.Marshal(manifest)
	if err != nil {
		return fmt.Errorf("error marshaling appstore version manifest: %w", err)
	}
	_, err = r.api.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName:                 aws.String(r.TableName),
		Key:                       key,
		ExpressionAttributeValues: map[string]types.AttributeValue{":m": value},
		UpdateExpression:          aws.String("set manifest = :m"),
	})
	if err != nil {
		return fmt.Errorf("error setting appstore version manifest: %w", err)
	}
	return nil
}

// SetVariants records a version's variants (defaultVariant, variantOrder and
// the variants map) and its top-level build settings. Empty values remove
// the attributes.
func (r *AppStoreVersionDatabaseStore) SetVariants(ctx context.Context, uuid string, v VersionVariants) error {
	key, err := attributevalue.MarshalMap(ApplicationKey{Uuid: uuid})
	if err != nil {
		return fmt.Errorf("error marshaling key for appstore version variants: %w", err)
	}
	values := map[string]types.AttributeValue{}
	var set, remove []string
	field := func(attr, placeholder string, empty bool, value any) error {
		if empty {
			remove = append(remove, attr)
			return nil
		}
		av, err := attributevalue.Marshal(value)
		if err != nil {
			return fmt.Errorf("error marshaling appstore version %s: %w", attr, err)
		}
		values[placeholder] = av
		set = append(set, attr+" = "+placeholder)
		return nil
	}
	if err := field("defaultVariant", ":d", v.DefaultVariant == "", v.DefaultVariant); err != nil {
		return err
	}
	if err := field("variantOrder", ":o", len(v.VariantOrder) == 0, v.VariantOrder); err != nil {
		return err
	}
	if err := field("variants", ":v", len(v.Variants) == 0, v.Variants); err != nil {
		return err
	}
	if err := field("build", ":b", v.Build == nil, v.Build); err != nil {
		return err
	}
	expr := ""
	if len(set) > 0 {
		expr = "set " + strings.Join(set, ", ")
	}
	if len(remove) > 0 {
		expr = strings.TrimSpace(expr + " remove " + strings.Join(remove, ", "))
	}
	in := &dynamodb.UpdateItemInput{
		TableName:        aws.String(r.TableName),
		Key:              key,
		UpdateExpression: aws.String(expr),
	}
	if len(values) > 0 {
		in.ExpressionAttributeValues = values
	}
	if _, err := r.api.UpdateItem(ctx, in); err != nil {
		return fmt.Errorf("error setting appstore version variants: %w", err)
	}
	return nil
}

package handler

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/expression"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/aws-sdk-go-v2/service/ecr"
	ecrtypes "github.com/aws/aws-sdk-go-v2/service/ecr/types"
	"github.com/pennsieve/app-deploy-service/status/models"
)

// variantTagSeparator joins a non-default variant's id to the release's
// image tag (fargate/app-provisioner/provisioner/imagebuild).
const variantTagSeparator = "--"

// isAppStoreBuild: only App Store builds point the status lambda at another
// table (the versions table); their applicationId is then the version's uuid.
func (h *DeployTaskStateChangeHandler) isAppStoreBuild(ids DeploymentApplicationIds) bool {
	return ids.ApplicationsTable != "" && ids.ApplicationsTable != h.ApplicationsTable
}

// variantImage is a variant's image, as the provisioner names it
// (imagebuild.VariantDestination): the version's own for the default variant
// or an app without variants, <image>--<id> for another.
func variantImage(destination string, ids DeploymentApplicationIds) string {
	if ids.Variant == "" || ids.VariantDefault {
		return destination
	}
	return destination + variantTagSeparator + ids.Variant
}

// recordImageDigest records the digest of the image a successful App Store
// build pushed: imageDigest on the version for an app without variants or
// the default variant, variants.<id>.imageDigest for a variant (the default
// gets both). Any failure is logged; it never fails the event or changes
// the build's status.
func (h *DeployTaskStateChangeHandler) recordImageDigest(ctx context.Context, ids DeploymentApplicationIds, tableName string) {
	if h.ECRApi == nil {
		return
	}
	logger := h.logger.With(slog.String("variant", ids.Variant))
	destination, err := h.versionDestination(ctx, ids.ApplicationId, tableName)
	if err != nil {
		logger.Warn("unable to read the version's image; its digest is not recorded", slog.Any("error", err))
		return
	}
	image := variantImage(destination, ids)
	digest, err := h.imageDigest(ctx, image)
	if err != nil {
		logger.Warn("unable to read the image's digest; it is not recorded", slog.String("image", image), slog.Any("error", err))
		return
	}
	logger.Info("recording image digest", slog.String("image", image), slog.String("digest", digest))
	if ids.Variant == "" || ids.VariantDefault {
		h.setImageDigest(ctx, logger, ids.ApplicationId, "", digest, tableName)
	}
	if ids.Variant != "" {
		h.setImageDigest(ctx, logger, ids.ApplicationId, ids.Variant, digest, tableName)
	}
}

// versionDestination is the version's destinationUrl: the image the
// provisioner pushed the release (or its default variant) to.
func (h *DeployTaskStateChangeHandler) versionDestination(ctx context.Context, versionId, tableName string) (string, error) {
	out, err := h.DynamoDBApi.GetItem(ctx, &dynamodb.GetItemInput{
		TableName:                aws.String(tableName),
		Key:                      models.ApplicationKey(versionId),
		ProjectionExpression:     aws.String("#d"),
		ExpressionAttributeNames: map[string]string{"#d": models.ApplicationDestinationUrlField},
	})
	if err != nil {
		return "", fmt.Errorf("error reading version %s: %w", versionId, err)
	}
	d, ok := out.Item[models.ApplicationDestinationUrlField].(*types.AttributeValueMemberS)
	if !ok || strings.TrimSpace(d.Value) == "" {
		return "", fmt.Errorf("version %s has no destinationUrl", versionId)
	}
	return strings.TrimSpace(d.Value), nil
}

// imageDigest asks ECR for the digest an image tag points to. The image is
// <account>.dkr.ecr.<region>.amazonaws.com/<repo>:<tag>.
func (h *DeployTaskStateChangeHandler) imageDigest(ctx context.Context, image string) (string, error) {
	registry, repoAndTag, ok := strings.Cut(image, "/")
	if !ok || !strings.Contains(registry, ".dkr.ecr.") {
		return "", fmt.Errorf("not an ECR image")
	}
	i := strings.LastIndex(repoAndTag, ":")
	if i < 0 || strings.Contains(repoAndTag, "@") {
		return "", fmt.Errorf("not an image tag")
	}
	repo, tag := repoAndTag[:i], repoAndTag[i+1:]
	out, err := h.ECRApi.DescribeImages(ctx, &ecr.DescribeImagesInput{
		RegistryId:     aws.String(strings.SplitN(registry, ".", 2)[0]),
		RepositoryName: aws.String(repo),
		ImageIds:       []ecrtypes.ImageIdentifier{{ImageTag: aws.String(tag)}},
	})
	if err != nil {
		return "", err
	}
	for _, d := range out.ImageDetails {
		if digest := aws.ToString(d.ImageDigest); digest != "" {
			return digest, nil
		}
	}
	return "", fmt.Errorf("tag %s not found in %s", tag, repo)
}

// setImageDigest sets imageDigest on the version (variant "") or on one of
// its variants, which must already be recorded.
func (h *DeployTaskStateChangeHandler) setImageDigest(ctx context.Context, logger *slog.Logger, versionId, variant, digest, tableName string) {
	in, err := ImageDigestUpdate(versionId, variant, digest, tableName)
	if err == nil {
		_, err = h.DynamoDBApi.UpdateItem(ctx, in)
	}
	if err != nil {
		logger.Warn("unable to record the image digest", slog.Bool("onVariant", variant != ""), slog.Any("error", err))
	}
}

// ImageDigestUpdate is the update setImageDigest makes.
func ImageDigestUpdate(versionId, variant, digest, tableName string) (*dynamodb.UpdateItemInput, error) {
	field, exists := models.ImageDigestField, models.ApplicationKeyField
	if variant != "" {
		field = models.VariantsField + "." + variant + "." + models.ImageDigestField
		exists = models.VariantsField + "." + variant
	}
	expressions, err := expression.NewBuilder().
		WithCondition(expression.AttributeExists(expression.Name(exists))).
		WithUpdate(expression.Set(expression.Name(field), expression.Value(digest))).Build()
	if err != nil {
		return nil, fmt.Errorf("error building the image digest update of %s: %w", versionId, err)
	}
	return &dynamodb.UpdateItemInput{
		Key:                       models.ApplicationKey(versionId),
		TableName:                 aws.String(tableName),
		ConditionExpression:       expressions.Condition(),
		ExpressionAttributeNames:  expressions.Names(),
		ExpressionAttributeValues: expressions.Values(),
		UpdateExpression:          expressions.Update(),
	}, nil
}

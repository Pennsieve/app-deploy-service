package handler

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/expression"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/aws-sdk-go-v2/service/ecr"
	ecrtypes "github.com/aws/aws-sdk-go-v2/service/ecr/types"
	"github.com/pennsieve/app-deploy-service/status/lambdaeligibility"
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
// gets both). With it, in the same update, it records whether the version
// (variant) can run as a Lambda step (lambda), from its stored manifest's
// runtime and the image (lambdaeligibility). Any failure is logged; it
// never fails the event or changes the build's status, and a failure to
// decide eligibility still records the digest.
func (h *DeployTaskStateChangeHandler) recordImageDigest(ctx context.Context, ids DeploymentApplicationIds, tableName string) {
	if h.ECRApi == nil {
		return
	}
	logger := h.logger.With(slog.String("variant", ids.Variant))
	rec, err := h.versionImageRecord(ctx, ids.ApplicationId, ids.Variant, tableName)
	if err != nil {
		logger.Warn("unable to read the version's image; its digest is not recorded", slog.Any("error", err))
		return
	}
	image := variantImage(rec.Destination, ids)
	digest, err := h.imageDigest(ctx, image)
	if err != nil {
		logger.Warn("unable to read the image's digest; it is not recorded", slog.String("image", image), slog.Any("error", err))
		return
	}
	logger.Info("recording image digest", slog.String("image", image), slog.String("digest", digest))
	facts, factsErr := lambdaeligibility.ReadImage(ctx, h.ECRApi, h.httpClient(), imageByDigest(image, digest))
	if factsErr != nil {
		logger.Warn("unable to read the image; its Lambda eligibility is not recorded", slog.String("image", image), slog.Any("error", factsErr))
	}
	eligibility := func(m *lambdaeligibility.Manifest) *lambdaeligibility.Eligibility {
		if factsErr != nil {
			return nil
		}
		e := decideEligibility(m, facts, time.Now())
		logger.Info("recording Lambda eligibility", slog.Bool("eligible", e.Eligible), slog.Any("reasons", e.Reasons))
		return &e
	}
	if ids.Variant == "" || ids.VariantDefault {
		h.setImageDigest(ctx, logger, ids.ApplicationId, "", digest, eligibility(rec.Manifest), tableName)
	}
	if ids.Variant != "" {
		m := rec.VariantManifest
		if m == nil {
			m = rec.Manifest
		}
		h.setImageDigest(ctx, logger, ids.ApplicationId, ids.Variant, digest, eligibility(m), tableName)
	}
}

// decideEligibility is a manifest's (nil: an app without app.yml, which
// declares nothing) eligibility with its image.
func decideEligibility(m *lambdaeligibility.Manifest, facts lambdaeligibility.ImageFacts, now time.Time) lambdaeligibility.Eligibility {
	var rt lambdaeligibility.Runtime
	if m != nil {
		rt = m.Runtime
	}
	return lambdaeligibility.Decide(rt, facts, m.HasReferenceInputs(), now)
}

// imageByDigest is the image by its digest: exactly what was pushed, even if
// the tag moves.
func imageByDigest(image, digest string) string {
	if i := strings.LastIndex(image, ":"); i > strings.LastIndex(image, "/") {
		image = image[:i]
	}
	return image + "@" + digest
}

func (h *DeployTaskStateChangeHandler) httpClient() lambdaeligibility.HTTPDoer {
	if h.HTTPClient != nil {
		return h.HTTPClient
	}
	return &http.Client{Timeout: 30 * time.Second}
}

// versionImage is what recording a build's image reads of its version: its
// image and the manifests (the version's, the variant's) eligibility needs.
type versionImage struct {
	Destination     string
	Manifest        *lambdaeligibility.Manifest
	VariantManifest *lambdaeligibility.Manifest
}

// versionImageRecord reads the version's destinationUrl (the image the
// provisioner pushed the release, or its default variant, to) and the
// runtime and inputs of its manifest and of the variant's.
func (h *DeployTaskStateChangeHandler) versionImageRecord(ctx context.Context, versionId, variant, tableName string) (versionImage, error) {
	var rec versionImage
	names := map[string]string{
		"#d":  models.ApplicationDestinationUrlField,
		"#m":  models.ManifestField,
		"#rt": models.ManifestRuntimeField,
		"#in": models.ManifestInputsField,
	}
	projection := "#d, #m.#rt, #m.#in"
	if variant != "" {
		names["#vs"], names["#v"] = models.VariantsField, variant
		projection += ", #vs.#v.#m.#rt, #vs.#v.#m.#in"
	}
	out, err := h.DynamoDBApi.GetItem(ctx, &dynamodb.GetItemInput{
		TableName:                aws.String(tableName),
		Key:                      models.ApplicationKey(versionId),
		ProjectionExpression:     aws.String(projection),
		ExpressionAttributeNames: names,
	})
	if err != nil {
		return rec, fmt.Errorf("error reading version %s: %w", versionId, err)
	}
	d, ok := out.Item[models.ApplicationDestinationUrlField].(*types.AttributeValueMemberS)
	if !ok || strings.TrimSpace(d.Value) == "" {
		return rec, fmt.Errorf("version %s has no destinationUrl", versionId)
	}
	rec.Destination = strings.TrimSpace(d.Value)
	var item struct {
		Manifest *lambdaeligibility.Manifest `dynamodbav:"manifest"`
		Variants map[string]struct {
			Manifest *lambdaeligibility.Manifest `dynamodbav:"manifest"`
		} `dynamodbav:"variants"`
	}
	// A manifest that can't be read is decided as one that declares
	// nothing; its image still is.
	if err := attributevalue.UnmarshalMap(out.Item, &item); err != nil {
		h.logger.Warn("unable to read the version's manifest", slog.String("version", versionId), slog.Any("error", err))
		return rec, nil
	}
	rec.Manifest = item.Manifest
	if variant != "" {
		rec.VariantManifest = item.Variants[variant].Manifest
	}
	return rec, nil
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

// setImageDigest sets imageDigest, and lambda when decided, on the version
// (variant "") or on one of its variants, which must already be recorded.
func (h *DeployTaskStateChangeHandler) setImageDigest(ctx context.Context, logger *slog.Logger, versionId, variant, digest string, lambda *lambdaeligibility.Eligibility, tableName string) {
	in, err := ImageDigestUpdate(versionId, variant, digest, lambda, tableName)
	if err == nil {
		_, err = h.DynamoDBApi.UpdateItem(ctx, in)
	}
	if err != nil {
		logger.Warn("unable to record the image digest", slog.Bool("onVariant", variant != ""), slog.Any("error", err))
	}
}

// ImageDigestUpdate is the update setImageDigest makes: imageDigest (unless
// digest is empty) and lambda (unless nil), on the version or a variant.
func ImageDigestUpdate(versionId, variant, digest string, lambda *lambdaeligibility.Eligibility, tableName string) (*dynamodb.UpdateItemInput, error) {
	prefix, exists := "", models.ApplicationKeyField
	if variant != "" {
		prefix = models.VariantsField + "." + variant + "."
		exists = models.VariantsField + "." + variant
	}
	var update expression.UpdateBuilder
	set := false
	if digest != "" {
		update, set = update.Set(expression.Name(prefix+models.ImageDigestField), expression.Value(digest)), true
	}
	if lambda != nil {
		update, set = update.Set(expression.Name(prefix+models.LambdaEligibilityField), expression.Value(lambda)), true
	}
	if !set {
		return nil, fmt.Errorf("nothing to record on %s", versionId)
	}
	expressions, err := expression.NewBuilder().
		WithCondition(expression.AttributeExists(expression.Name(exists))).
		WithUpdate(update).Build()
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

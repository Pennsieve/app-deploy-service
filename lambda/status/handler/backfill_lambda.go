package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/pennsieve/app-deploy-service/status/lambdaeligibility"
	"github.com/pennsieve/app-deploy-service/status/models"
)

// VersionsTableApi is what the Lambda eligibility backfill uses of the
// versions table.
type VersionsTableApi interface {
	Scan(ctx context.Context, params *dynamodb.ScanInput, optFns ...func(*dynamodb.Options)) (*dynamodb.ScanOutput, error)
	UpdateItem(ctx context.Context, params *dynamodb.UpdateItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.UpdateItemOutput, error)
}

// LambdaBackfillResult counts what a backfill did: images decided, of
// versions and variants, and those whose image couldn't be read.
type LambdaBackfillResult struct {
	Versions, Variants, Eligible, Ineligible, Unreadable int
}

// backfillVersion is what the backfill reads of a version.
type backfillVersion struct {
	Uuid           string                      `dynamodbav:"uuid"`
	DestinationUrl string                      `dynamodbav:"destinationUrl"`
	ImageDigest    string                      `dynamodbav:"imageDigest"`
	Manifest       *lambdaeligibility.Manifest `dynamodbav:"manifest"`
	DefaultVariant string                      `dynamodbav:"defaultVariant"`
	Variants       map[string]struct {
		DestinationUrl string                      `dynamodbav:"destinationUrl"`
		ImageDigest    string                      `dynamodbav:"imageDigest"`
		Manifest       *lambdaeligibility.Manifest `dynamodbav:"manifest"`
	} `dynamodbav:"variants"`
}

// BackfillLambdaEligibility decides and records lambda on every version of
// the versions table with an image, and on each of its variants, as the
// status lambda does after a build: from the stored manifest's runtime and
// the image in ECR (by its recorded digest, else its tag). A version or
// variant whose image can't be read (never built, deleted) is logged and
// left as it is. With dryRun nothing is written; each decision is logged.
func BackfillLambdaEligibility(ctx context.Context, db VersionsTableApi, images lambdaeligibility.ImageReader, httpc lambdaeligibility.HTTPDoer,
	tableName string, dryRun bool, now func() time.Time, logf func(string, ...any)) (LambdaBackfillResult, error) {
	var res LambdaBackfillResult
	facts := map[string]lambdaeligibility.ImageFacts{}
	failed := map[string]bool{}
	read := func(image string) (lambdaeligibility.ImageFacts, bool) {
		if f, ok := facts[image]; ok {
			return f, true
		}
		if failed[image] {
			return lambdaeligibility.ImageFacts{}, false
		}
		f, err := lambdaeligibility.ReadImage(ctx, images, httpc, image)
		if err != nil {
			logf("%s: unable to read the image: %v", image, err)
			failed[image] = true
			return f, false
		}
		facts[image] = f
		return f, true
	}
	record := func(version, variant, image string, m *lambdaeligibility.Manifest) error {
		f, ok := read(image)
		if !ok {
			res.Unreadable++
			return nil
		}
		e := decideEligibility(m, f, now())
		if e.Eligible {
			res.Eligible++
		} else {
			res.Ineligible++
		}
		where := version
		if variant != "" {
			where += " variant " + variant
		}
		out, _ := json.Marshal(e)
		logf("%s (%s): lambda %s", where, image, out)
		if dryRun {
			return nil
		}
		in, err := ImageDigestUpdate(version, variant, "", &e, tableName)
		if err == nil {
			_, err = db.UpdateItem(ctx, in)
		}
		if err != nil {
			return fmt.Errorf("recording the Lambda eligibility of %s: %w", where, err)
		}
		return nil
	}

	var start map[string]types.AttributeValue
	for {
		out, err := db.Scan(ctx, &dynamodb.ScanInput{TableName: aws.String(tableName), ExclusiveStartKey: start})
		if err != nil {
			return res, fmt.Errorf("scanning %s: %w", tableName, err)
		}
		for _, item := range out.Items {
			var v backfillVersion
			if err := attributevalue.UnmarshalMap(item, &v); err != nil {
				logf("%v: unable to read the version: %v", item[models.ApplicationKeyField], err)
				continue
			}
			if v.DestinationUrl == "" {
				continue
			}
			res.Versions++
			if err := record(v.Uuid, "", versionImageRef(v.DestinationUrl, v.ImageDigest), v.Manifest); err != nil {
				return res, err
			}
			ids := make([]string, 0, len(v.Variants))
			for id := range v.Variants {
				ids = append(ids, id)
			}
			sort.Strings(ids)
			for _, id := range ids {
				sv := v.Variants[id]
				res.Variants++
				destination := sv.DestinationUrl
				if destination == "" {
					destination = variantImage(v.DestinationUrl, DeploymentApplicationIds{Variant: id, VariantDefault: id == v.DefaultVariant})
				}
				m := sv.Manifest
				if m == nil {
					m = v.Manifest
				}
				if err := record(v.Uuid, id, versionImageRef(destination, sv.ImageDigest), m); err != nil {
					return res, err
				}
			}
		}
		if len(out.LastEvaluatedKey) == 0 {
			return res, nil
		}
		start = out.LastEvaluatedKey
	}
}

// versionImageRef is an image by its recorded digest, or by its tag before
// one was recorded.
func versionImageRef(image, digest string) string {
	if digest == "" {
		return image
	}
	return imageByDigest(image, digest)
}

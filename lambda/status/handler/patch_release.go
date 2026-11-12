package handler

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/pennsieve/app-deploy-service/status/models"
)

// Patch releases (docs/builds.md "Sample runs", "Patch releases"). A patch
// release (vX.Y.Z with Z > 0 and no pre-release) of an app whose earlier
// release in the same X.Y line already has a release report is not run on
// the sample runner: sampleRun.status = skipped with patch_release, the
// release event goes out at once (curation-service makes no report), and the
// app page keeps showing the earlier version's report. The owner can still
// make one from the version's row ("Generate release report").

// VersionsByApplicationIndex is the versions table's index by application
// and version (terraform/dynamodb.tf).
const VersionsByApplicationIndex = "applicationId-version-index"

// dynamoQuerier is the part of the DynamoDB client the patch rule needs;
// without it (a client that can't query) the rule never applies.
type dynamoQuerier interface {
	Query(ctx context.Context, params *dynamodb.QueryInput, optFns ...func(*dynamodb.Options)) (*dynamodb.QueryOutput, error)
}

// releaseNumbers reads "v1.2.3" or "1.2.3" into its three numbers; ok is
// false for anything else, a pre-release or build suffix included.
func releaseNumbers(tag string) (nums [3]int, ok bool) {
	t := strings.TrimPrefix(strings.TrimSpace(tag), "v")
	parts := strings.Split(t, ".")
	if len(parts) != 3 {
		return nums, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 || strconv.Itoa(n) != p {
			return nums, false
		}
		nums[i] = n
	}
	return nums, true
}

// reportedPatchBase is the tag of an earlier release of the same app, in the
// same major.minor line as rel, that has a ready release report; "" when rel
// is not a patch release or no such release exists. The newest such release
// is returned.
func (h *DeployTaskStateChangeHandler) reportedPatchBase(ctx context.Context, rel release, versionsTable string) (string, error) {
	cur, ok := releaseNumbers(rel.Tag)
	if !ok || cur[2] == 0 {
		return "", nil
	}
	q, ok := h.DynamoDBApi.(dynamoQuerier)
	if !ok {
		return "", nil
	}
	in := &dynamodb.QueryInput{
		TableName:                aws.String(versionsTable),
		IndexName:                aws.String(VersionsByApplicationIndex),
		KeyConditionExpression:   aws.String("#a = :a"),
		ProjectionExpression:     aws.String("#u, #v, #s.#st"),
		ExpressionAttributeNames: map[string]string{"#a": "applicationId", "#u": "uuid", "#v": "version", "#s": models.SampleField, "#st": "status"},
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":a": &types.AttributeValueMemberS{Value: rel.ApplicationId},
		},
	}
	best, bestTag := -1, ""
	for {
		out, err := q.Query(ctx, in)
		if err != nil {
			return "", fmt.Errorf("error listing the versions of %s: %w", rel.ApplicationId, err)
		}
		for _, item := range out.Items {
			var v struct {
				Uuid    string `dynamodbav:"uuid"`
				Version string `dynamodbav:"version"`
				Sample  *struct {
					Status string `dynamodbav:"status"`
				} `dynamodbav:"sample"`
			}
			if err := attributevalue.UnmarshalMap(item, &v); err != nil || v.Uuid == rel.VersionId {
				continue
			}
			if v.Sample == nil || v.Sample.Status != models.SampleDocReady {
				continue
			}
			n, ok := releaseNumbers(v.Version)
			if !ok || n[0] != cur[0] || n[1] != cur[1] || n[2] >= cur[2] {
				continue
			}
			if n[2] > best {
				best, bestTag = n[2], v.Version
			}
		}
		if len(out.LastEvaluatedKey) == 0 {
			break
		}
		in.ExclusiveStartKey = out.LastEvaluatedKey
	}
	return bestTag, nil
}

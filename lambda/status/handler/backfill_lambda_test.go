package handler

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// pagedVersions scans its items one page per item.
type pagedVersions struct {
	items   []map[string]types.AttributeValue
	updates []*dynamodb.UpdateItemInput
}

func (p *pagedVersions) Scan(_ context.Context, in *dynamodb.ScanInput, _ ...func(*dynamodb.Options)) (*dynamodb.ScanOutput, error) {
	i := 0
	if in.ExclusiveStartKey != nil {
		fmt.Sscan(in.ExclusiveStartKey["page"].(*types.AttributeValueMemberN).Value, &i)
	}
	out := &dynamodb.ScanOutput{Items: p.items[i : i+1]}
	if i+1 < len(p.items) {
		out.LastEvaluatedKey = map[string]types.AttributeValue{"page": &types.AttributeValueMemberN{Value: fmt.Sprint(i + 1)}}
	}
	return out, nil
}

func (p *pagedVersions) UpdateItem(_ context.Context, in *dynamodb.UpdateItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.UpdateItemOutput, error) {
	p.updates = append(p.updates, in)
	return &dynamodb.UpdateItemOutput{}, nil
}

func backfillTable() *pagedVersions {
	withDigest := versionWithGPUVariant()
	withDigest["uuid"] = &types.AttributeValueMemberS{Value: "version-1"}
	withDigest["imageDigest"] = &types.AttributeValueMemberS{Value: "sha256:cpu"}
	withDigest["defaultVariant"] = &types.AttributeValueMemberS{Value: "cpu"}
	withDigest["variants"].(*types.AttributeValueMemberM).Value["gpu"].(*types.AttributeValueMemberM).Value["imageDigest"] = &types.AttributeValueMemberS{Value: "sha256:gpu"}
	return &pagedVersions{items: []map[string]types.AttributeValue{
		withDigest,
		// never built: no image to read
		{"uuid": &types.AttributeValueMemberS{Value: "version-2"}, "destinationUrl": &types.AttributeValueMemberS{Value: testDestination + "-missing"}},
		// no image at all
		{"uuid": &types.AttributeValueMemberS{Value: "version-3"}},
	}}
}

func TestBackfillLambdaEligibility(t *testing.T) {
	fake := fakeLambdaImage(t)
	table := backfillTable()
	var logs []string
	now := func() time.Time { return time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC) }
	res, err := BackfillLambdaEligibility(context.Background(), table, fake, fake.blobs.Client(), testVersionsTable, false, now,
		func(f string, a ...any) { logs = append(logs, fmt.Sprintf(f, a...)) })
	require.NoError(t, err)
	assert.Equal(t, LambdaBackfillResult{Versions: 2, Variants: 1, Eligible: 1, Ineligible: 1, Unreadable: 1}, res)

	require.Len(t, table.updates, 2)
	l := lambdaValue(t, table.updates[0], "lambda")
	assert.Equal(t, &types.AttributeValueMemberBOOL{Value: true}, l["eligible"])
	assert.Equal(t, "2026-11-03T12:00:00Z", stringValue(t, l["checkedAt"]))
	assert.NotContains(t, readable(aws.ToString(table.updates[0].UpdateExpression), table.updates[0]), "imageDigest", "only lambda")
	l = lambdaValue(t, table.updates[1], "variants.gpu.lambda")
	assert.Equal(t, []string{"it runs on GPU only"}, reasons(t, l))
	assert.Equal(t, "sha256:gpu", aws.ToString(fake.manifestReads[1].ImageIds[0].ImageDigest), "the variant's image, by its digest")
	assert.Contains(t, logs[0], `version-1 (`+"123456789012.dkr.ecr.us-east-1.amazonaws.com/"+testImageRepo+`@sha256:cpu): lambda {"eligible":true`)

	// a dry run writes nothing
	table = backfillTable()
	res, err = BackfillLambdaEligibility(context.Background(), table, fake, fake.blobs.Client(), testVersionsTable, true, now, func(string, ...any) {})
	require.NoError(t, err)
	assert.Equal(t, 1, res.Eligible)
	assert.Empty(t, table.updates)
}

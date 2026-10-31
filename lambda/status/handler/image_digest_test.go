package handler

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/aws-sdk-go-v2/service/ecr"
	ecrtypes "github.com/aws/aws-sdk-go-v2/service/ecr/types"
	"github.com/pennsieve/app-deploy-service/status/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testImageRepo   = "dev-app-store-use1"
	testImageTag    = "5f1c0a-v1.0.0"
	testDestination = "123456789012.dkr.ecr.us-east-1.amazonaws.com/" + testImageRepo + ":" + testImageTag
)

// fakeECR knows the digests of some image tags.
type fakeECR struct {
	digests map[string]string // tag → digest
	err     error
	calls   []*ecr.DescribeImagesInput
}

func (f *fakeECR) DescribeImages(_ context.Context, in *ecr.DescribeImagesInput, _ ...func(*ecr.Options)) (*ecr.DescribeImagesOutput, error) {
	f.calls = append(f.calls, in)
	if f.err != nil {
		return nil, f.err
	}
	tag := aws.ToString(in.ImageIds[0].ImageTag)
	digest, ok := f.digests[tag]
	if !ok {
		return nil, &ecrtypes.ImageNotFoundException{Message: aws.String("not found")}
	}
	return &ecr.DescribeImagesOutput{ImageDetails: []ecrtypes.ImageDetail{{ImageDigest: aws.String(digest), ImageTags: []string{tag}}}}, nil
}

// digestUpdates spells out the version updates that set an image digest,
// with their conditions.
func digestUpdates(db *recordingDynamoDB) []string {
	var out []string
	for _, u := range db.tableUpdates(testVersionsTable) {
		if expr := readable(aws.ToString(u.UpdateExpression), u); strings.Contains(expr, models.ImageDigestField) {
			out = append(out, strings.TrimSpace(expr)+" IF "+readable(aws.ToString(u.ConditionExpression), u))
		}
	}
	return out
}

func TestHandleBuild_RecordsImageDigest(t *testing.T) {
	ecrDigests := map[string]string{
		testImageTag:           "sha256:cpu",
		testImageTag + "--gpu": "sha256:gpu",
	}
	cases := []struct {
		name         string
		event        []byte
		destination  string
		ecrErr       error
		wantTag      string
		wantUpdates  []string
		wantStatuses int // version updates besides the digests
	}{
		{
			name:         "an app without variants",
			event:        buildEvent(models.BuildSucceeded, true),
			destination:  testDestination,
			wantTag:      testImageTag,
			wantUpdates:  []string{`SET imageDigest = "sha256:cpu" IF attribute_exists (uuid)`},
			wantStatuses: 1,
		},
		{
			name:        "the default variant: the version's and the variant's",
			event:       variantBuildEvent(models.BuildSucceeded, "cpu", true),
			destination: testDestination,
			wantTag:     testImageTag,
			wantUpdates: []string{
				`SET imageDigest = "sha256:cpu" IF attribute_exists (uuid)`,
				`SET variants.cpu.imageDigest = "sha256:cpu" IF attribute_exists (variants.cpu)`,
			},
			wantStatuses: 2,
		},
		{
			name:         "another variant: only its own, of its own image",
			event:        variantBuildEvent(models.BuildSucceeded, "gpu", false),
			destination:  testDestination,
			wantTag:      testImageTag + "--gpu",
			wantUpdates:  []string{`SET variants.gpu.imageDigest = "sha256:gpu" IF attribute_exists (variants.gpu)`},
			wantStatuses: 1,
		},
		{
			name:         "ECR fails: the build is still deployed",
			event:        buildEvent(models.BuildSucceeded, true),
			destination:  testDestination,
			ecrErr:       errors.New("AccessDeniedException"),
			wantTag:      testImageTag,
			wantStatuses: 1,
		},
		{
			name:         "the tag isn't there",
			event:        variantBuildEvent(models.BuildSucceeded, "arm", false),
			destination:  testDestination,
			wantTag:      testImageTag + "--arm",
			wantStatuses: 1,
		},
		{
			name:         "no destinationUrl on the version",
			event:        buildEvent(models.BuildSucceeded, true),
			wantStatuses: 1,
		},
		{
			name:         "a failed build has no image",
			event:        buildEvent(models.BuildFailed, true),
			destination:  testDestination,
			wantStatuses: 1,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h, db, _, _, _ := newBuildTestHandler()
			if c.destination != "" {
				db.items = map[string]map[string]types.AttributeValue{testVersionsTable: {
					models.ApplicationDestinationUrlField: &types.AttributeValueMemberS{Value: c.destination},
				}}
			}
			fake := &fakeECR{digests: ecrDigests, err: c.ecrErr}
			h.WithECR(fake)

			require.NoError(t, h.HandleEvent(context.Background(), c.event), "a digest never fails the event")

			assert.Equal(t, c.wantUpdates, digestUpdates(db))
			assert.Len(t, db.tableUpdates(testVersionsTable), len(c.wantUpdates)+c.wantStatuses, "the status is recorded as always")
			if c.wantTag == "" {
				assert.Empty(t, fake.calls)
				return
			}
			require.Len(t, fake.calls, 1)
			assert.Equal(t, testImageRepo, aws.ToString(fake.calls[0].RepositoryName))
			assert.Equal(t, "123456789012", aws.ToString(fake.calls[0].RegistryId))
			assert.Equal(t, c.wantTag, aws.ToString(fake.calls[0].ImageIds[0].ImageTag))
			require.Len(t, db.gets, 1)
			assert.Equal(t, models.ApplicationKey("version-1"), db.gets[0].Key)
		})
	}
}

// The digest is on the version before its status says deployed.
func TestHandleBuild_DigestBeforeStatus(t *testing.T) {
	h, db, _, _, _ := newBuildTestHandler()
	db.items = map[string]map[string]types.AttributeValue{testVersionsTable: {
		models.ApplicationDestinationUrlField: &types.AttributeValueMemberS{Value: testDestination},
	}}
	h.WithECR(&fakeECR{digests: map[string]string{testImageTag: "sha256:cpu"}})
	require.NoError(t, h.HandleEvent(context.Background(), buildEvent(models.BuildSucceeded, true)))
	versions := db.tableUpdates(testVersionsTable)
	require.Len(t, versions, 2)
	assert.Contains(t, readable(aws.ToString(versions[0].UpdateExpression), versions[0]), "imageDigest")
	assert.Equal(t, "deployed", stringValue(t, setValues(versions[1])[models.ApplicationStatusField]))
}

// A build of a workspace application (not the App Store) is untouched: no
// lookups, no digest.
func TestHandleEvent_NonAppStoreBuildRecordsNoDigest(t *testing.T) {
	db := &recordingDynamoDB{}
	fake := &fakeECR{digests: map[string]string{testImageTag: "sha256:cpu"}}
	h := NewDeployTaskStateChangeHandler(fakeECS{}, db, testApplicationsTable, testDeploymentsTable).WithECR(fake)
	event := []byte(`{
		"source": "aws.ecs",
		"detail-type": "ECS Task State Change",
		"detail": {
			"taskArn": "arn:aws:ecs:us-east-1:123456789012:task/c/abc",
			"clusterArn": "arn:aws:ecs:us-east-1:123456789012:cluster/c",
			"lastStatus": "STOPPED",
			"desiredStatus": "STOPPED",
			"version": 4,
			"containers": [{"exitCode": 0}]
		}
	}`)
	require.NoError(t, h.HandleEvent(context.Background(), event))
	assert.Empty(t, fake.calls)
	assert.Empty(t, db.gets)
	apps := db.tableUpdates(testApplicationsTable)
	require.Len(t, apps, 1)
	assert.Equal(t, "deployed", stringValue(t, setValues(apps[0])[models.ApplicationStatusField]))
}

func TestVariantImage(t *testing.T) {
	cases := []struct {
		variant   string
		isDefault bool
		want      string
	}{
		{"", false, "repo:1-v1"},
		{"cpu", true, "repo:1-v1"},
		{"gpu", false, "repo:1-v1--gpu"},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, variantImage("repo:1-v1", DeploymentApplicationIds{Variant: c.variant, VariantDefault: c.isDefault}))
	}
}

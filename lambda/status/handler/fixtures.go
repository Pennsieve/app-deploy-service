package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/expression"
	"github.com/aws/aws-sdk-go-v2/service/codebuild"
	cbtypes "github.com/aws/aws-sdk-go-v2/service/codebuild/types"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/eventbridge"
	ebtypes "github.com/aws/aws-sdk-go-v2/service/eventbridge/types"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/pennsieve/app-deploy-service/status/external"
	"github.com/pennsieve/app-deploy-service/status/models"
)

// Fixture bundles (docs/builds.md "Fixture bundles"). When a release's image
// is deployed (built, promoted, its digest recorded), the status lambda
// starts a build of the fixture-builder project for the version, unless the
// app declares no review block, its repository is private, or it is
// app-fixtures itself; then the version records why and the release event
// goes out at once. The fixture build's own state change comes back here:
// its manifest.json becomes the version's fixtures record, bundles of
// releases before the previous one are tagged for expiry, and the release
// event goes out. Every step is best effort against the image build: a
// fixture problem never changes a version's deployed status.

// Environment variables (main.go).
const (
	FixtureBuilderProjectEnvVar     = "FIXTURE_BUILDER_PROJECT"
	FixturesBucketEnvVar            = "FIXTURES_BUCKET"
	AppStoreApplicationsTableEnvVar = "APPSTORE_APPLICATIONS_TABLE"
	EnvironmentEnvVar               = "ENV"
	AppFixturesRepositoryEnvVar     = "APP_FIXTURES_REPOSITORY"
)

// DefaultAppFixturesRepository is the platform app whose own releases get
// no fixture build.
const DefaultAppFixturesRepository = "https://github.com/pennsieve-apps/app-fixtures"

// Variables a fixture build gets. fixtures.sh reads REPOSITORY, TAG,
// APPLICATION_ID (the App Store application: the bundle's prefix) and
// PORTS; VERSION_ID and APPLICATIONS_TABLE name the version to record on.
const (
	FixtureRepositoryVar    = "REPOSITORY"
	FixtureTagVar           = "TAG"
	FixtureApplicationIdVar = "APPLICATION_ID"
	FixturePortsVar         = "PORTS"
	FixtureVersionIdVar     = "VERSION_ID"
)

// fixturesKeyPrefix is where an app's bundles live (fixtures.sh).
const fixturesKeyPrefix = "fixtures/"

// retainTag marks a bundle the bucket's lifecycle may expire.
const retainTagKey, retainTagNo = "retain", "no"

// FixtureBuilder is the status lambda's side of fixture bundles; nil when
// the environment has no fixture builder.
type FixtureBuilder struct {
	Project                   string
	Bucket                    string
	AppStoreApplicationsTable string
	Environment               string
	AppFixturesRepository     string
	CodeBuild                 external.FixtureBuildApi
	S3                        external.S3Api
	Events                    external.EventsApi
	// Now is the clock; nil: time.Now.
	Now func() time.Time
}

func (fb *FixtureBuilder) now() string {
	if fb.Now != nil {
		return fb.Now().UTC().Format(time.RFC3339)
	}
	return time.Now().UTC().Format(time.RFC3339)
}

// WithFixtureBuilder lets the handler build fixture bundles for deployed
// versions and record their outcome.
func (h *DeployTaskStateChangeHandler) WithFixtureBuilder(fb *FixtureBuilder) *DeployTaskStateChangeHandler {
	if fb != nil && fb.AppFixturesRepository == "" {
		fb.AppFixturesRepository = DefaultAppFixturesRepository
	}
	h.Fixtures = fb
	return h
}

// isFixtureBuild: a Build State Change event of the fixture-builder project.
func (h *DeployTaskStateChangeHandler) isFixtureBuild(event models.BuildStateChangeEvent) bool {
	return h.Fixtures != nil && h.Fixtures.Project != "" && event.Detail.ProjectName == h.Fixtures.Project
}

// release is what the fixture steps need to know about a version and its app.
type release struct {
	VersionId     string
	ApplicationId string
	Tag           string
	ImageDigest   string
	HasReview     bool
	SourceUrl     string
	Visibility    string
	IsPrivate     bool
}

// releaseFacts reads the version (versions table) and its App Store
// application.
func (h *DeployTaskStateChangeHandler) releaseFacts(ctx context.Context, versionId, versionsTable string) (release, error) {
	rel := release{VersionId: versionId}
	out, err := h.DynamoDBApi.GetItem(ctx, &dynamodb.GetItemInput{
		TableName:                aws.String(versionsTable),
		Key:                      models.ApplicationKey(versionId),
		ProjectionExpression:     aws.String("#a, #v, #d, #r"),
		ExpressionAttributeNames: map[string]string{"#a": "applicationId", "#v": "version", "#d": models.ImageDigestField, "#r": "review"},
	})
	if err != nil {
		return rel, fmt.Errorf("error reading version %s: %w", versionId, err)
	}
	if len(out.Item) == 0 {
		return rel, fmt.Errorf("version %s not found", versionId)
	}
	var v struct {
		ApplicationId string         `dynamodbav:"applicationId"`
		Version       string         `dynamodbav:"version"`
		ImageDigest   string         `dynamodbav:"imageDigest"`
		Review        map[string]any `dynamodbav:"review"`
	}
	if err := attributevalue.UnmarshalMap(out.Item, &v); err != nil {
		return rel, fmt.Errorf("error reading version %s: %w", versionId, err)
	}
	rel.ApplicationId, rel.Tag, rel.ImageDigest, rel.HasReview = v.ApplicationId, v.Version, v.ImageDigest, len(v.Review) > 0
	if rel.ApplicationId == "" || rel.Tag == "" {
		return rel, fmt.Errorf("version %s has no applicationId or version", versionId)
	}
	app, err := h.DynamoDBApi.GetItem(ctx, &dynamodb.GetItemInput{
		TableName:                aws.String(h.Fixtures.AppStoreApplicationsTable),
		Key:                      models.ApplicationKey(rel.ApplicationId),
		ProjectionExpression:     aws.String("#s, #vis, #p"),
		ExpressionAttributeNames: map[string]string{"#s": "sourceUrl", "#vis": "visibility", "#p": "isPrivate"},
	})
	if err != nil {
		return rel, fmt.Errorf("error reading application %s: %w", rel.ApplicationId, err)
	}
	var a struct {
		SourceUrl  string `dynamodbav:"sourceUrl"`
		Visibility string `dynamodbav:"visibility"`
		IsPrivate  bool   `dynamodbav:"isPrivate"`
	}
	if err := attributevalue.UnmarshalMap(app.Item, &a); err != nil {
		return rel, fmt.Errorf("error reading application %s: %w", rel.ApplicationId, err)
	}
	rel.SourceUrl, rel.Visibility, rel.IsPrivate = strings.TrimSpace(a.SourceUrl), a.Visibility, a.IsPrivate
	if rel.SourceUrl == "" {
		return rel, fmt.Errorf("application %s has no sourceUrl", rel.ApplicationId)
	}
	return rel, nil
}

// sameRepository compares repository URLs without a trailing .git or slash.
func sameRepository(a, b string) bool {
	norm := func(s string) string {
		s = strings.TrimSuffix(strings.TrimSuffix(strings.TrimSpace(s), "/"), ".git")
		return strings.ToLower(s)
	}
	return norm(a) == norm(b)
}

// startFixtureBuild runs after an App Store version is deployed (an app
// without variants, or its default variant). Best effort.
func (h *DeployTaskStateChangeHandler) startFixtureBuild(ctx context.Context, logger *slog.Logger, versionId, versionsTable string) {
	fb := h.Fixtures
	if fb == nil || fb.Project == "" {
		return
	}
	rel, err := h.releaseFacts(ctx, versionId, versionsTable)
	if err != nil {
		logger.Warn("fixtures: unable to read the release; no fixture build", slog.Any("error", err))
		return
	}
	logger = logger.With(slog.String("applicationId", rel.ApplicationId), slog.String("tag", rel.Tag))
	skip := func(code, message string) {
		logger.Info("fixtures: skipped", slog.String("reason", code))
		rec := models.Fixtures{Status: models.FixturesSkipped, Error: &models.FixturesError{Code: code, Message: message}, UpdatedAt: fb.now()}
		h.setFixtures(ctx, logger, rel, versionsTable, rec)
		h.emitReleaseReady(ctx, logger, rel, rec)
	}
	switch {
	case sameRepository(rel.SourceUrl, fb.AppFixturesRepository):
		skip(models.FixturesErrorPlatformApp, "app-fixtures builds the fixtures of other apps and gets none of its own")
	case rel.IsPrivate:
		skip(models.FixturesErrorPrivateRepository, "fixtures are built from public repositories only")
	case !rel.HasReview:
		skip(models.FixturesErrorNoReviewBlock, "app.yml declares no review block, so the app has no test inputs to build")
	default:
		h.runFixtureBuild(ctx, logger, rel, versionsTable)
	}
}

func (h *DeployTaskStateChangeHandler) runFixtureBuild(ctx context.Context, logger *slog.Logger, rel release, versionsTable string) {
	fb := h.Fixtures
	plain := func(name, value string) cbtypes.EnvironmentVariable {
		return cbtypes.EnvironmentVariable{Name: aws.String(name), Value: aws.String(value), Type: cbtypes.EnvironmentVariableTypePlaintext}
	}
	out, err := fb.CodeBuild.StartBuild(ctx, &codebuild.StartBuildInput{
		ProjectName: aws.String(fb.Project),
		EnvironmentVariablesOverride: []cbtypes.EnvironmentVariable{
			plain(FixtureRepositoryVar, rel.SourceUrl),
			plain(FixtureTagVar, rel.Tag),
			plain(FixtureApplicationIdVar, rel.ApplicationId),
			plain(FixtureVersionIdVar, rel.VersionId),
			plain(BuildApplicationsTableVar, versionsTable),
		},
	})
	if err != nil {
		logger.Warn("fixtures: unable to start the fixture build", slog.Any("error", err))
		rec := models.Fixtures{Status: models.FixturesFailed, Error: &models.FixturesError{Code: models.FixturesErrorBuildFailed, Message: "the fixture build could not be started: " + err.Error()}, UpdatedAt: fb.now()}
		h.setFixtures(ctx, logger, rel, versionsTable, rec)
		h.emitReleaseReady(ctx, logger, rel, rec)
		return
	}
	buildId := ""
	if out != nil && out.Build != nil {
		buildId = aws.ToString(out.Build.Id)
	}
	logger.Info("fixtures: build started", slog.String("fixtureBuildId", buildId))
	h.setFixtures(ctx, logger, rel, versionsTable, models.Fixtures{Status: models.FixturesBuilding, BuildId: buildId, StartedAt: fb.now(), UpdatedAt: fb.now()})
}

// HandleFixtureBuild records how a fixture build ended on its version and
// emits the release event. IN_PROGRESS is ignored.
func (h *DeployTaskStateChangeHandler) HandleFixtureBuild(ctx context.Context, event models.BuildStateChangeEvent) error {
	fb := h.Fixtures
	logger := h.logger.With(slog.String("fixtureBuildId", event.Detail.BuildId), slog.String("buildStatus", event.Detail.BuildStatus))
	failed, final := buildFinalStates[event.Detail.BuildStatus]
	if !final {
		if event.Detail.BuildStatus != models.BuildInProgress {
			logger.Warn("ignoring fixture build event with unknown status")
		}
		return nil
	}
	vars, err := h.fixtureBuildVariables(ctx, event)
	if err != nil {
		return err
	}
	versionId, versionsTable := vars[FixtureVersionIdVar], vars[BuildApplicationsTableVar]
	applicationId, tag := vars[FixtureApplicationIdVar], vars[FixtureTagVar]
	if versionId == "" || versionsTable == "" || applicationId == "" || tag == "" {
		return fmt.Errorf("fixture build %s is missing its version variables", event.Detail.BuildId)
	}
	logger = logger.With(slog.String("versionId", versionId), slog.String("applicationId", applicationId), slog.String("tag", tag))

	rec := models.Fixtures{BuildId: event.Detail.BuildId, UpdatedAt: fb.now()}
	if failed {
		rec.Status = models.FixturesFailed
		rec.Error = &models.FixturesError{
			Code:    "build_" + strings.ToLower(event.Detail.BuildStatus),
			Message: h.buildFailureReason(ctx, event, "fixture build"),
		}
	} else if manifest, err := h.readFixtureManifest(ctx, applicationId, tag); err != nil {
		logger.Warn("fixtures: the build succeeded but its manifest could not be read", slog.Any("error", err))
		rec.Status = models.FixturesFailed
		rec.Error = &models.FixturesError{Code: models.FixturesErrorManifestMissing, Message: err.Error()}
	} else {
		rec = fixturesFromManifest(manifest, rec, fb.Bucket)
	}
	rel := release{VersionId: versionId, ApplicationId: applicationId, Tag: tag}
	h.setFixtures(ctx, logger, rel, versionsTable, rec)
	if rec.Status == models.FixturesReady {
		h.expireSupersededBundles(ctx, logger, applicationId, tag)
	}
	// The event carries the release's image and visibility too.
	if full, err := h.releaseFacts(ctx, versionId, versionsTable); err == nil {
		rel = full
	} else {
		logger.Warn("fixtures: unable to read the release for its event; sending what is known", slog.Any("error", err))
	}
	h.emitReleaseReady(ctx, logger, rel, rec)
	logger.Info("fixtures: recorded", slog.String("status", rec.Status))
	return nil
}

// fixturesFromManifest is the version's record of a finished fixture build.
func fixturesFromManifest(m models.FixturesManifest, rec models.Fixtures, bucket string) models.Fixtures {
	rec.Warnings, rec.Files, rec.StartedAt, rec.BuiltAt = m.Warnings, m.Files, m.StartedAt, m.BuiltAt
	if m.BuildId != "" {
		rec.BuildId = m.BuildId
	}
	switch {
	case m.Status == models.FixturesReady && m.Bundle != nil && m.Bundle.Key != "":
		rec.Status = models.FixturesReady
		rec.Bucket, rec.Key, rec.Bytes, rec.Sha256 = bucket, m.Bundle.Key, m.Bundle.Bytes, m.Bundle.Sha256
	default:
		rec.Status = models.FixturesFailed
		rec.Error = m.Error
		if rec.Error == nil {
			rec.Error = &models.FixturesError{Code: "internal", Message: "the fixture build's manifest names no bundle and no error"}
		}
	}
	return rec
}

// fixtureBuildVariables are the build's variables, from the event or from
// the build itself.
func (h *DeployTaskStateChangeHandler) fixtureBuildVariables(ctx context.Context, event models.BuildStateChangeEvent) (map[string]string, error) {
	values := func(env models.BuildEnvironment) map[string]string {
		m := map[string]string{}
		for _, v := range env.EnvironmentVariables {
			m[v.Name] = strings.TrimSpace(v.Value)
		}
		return m
	}
	if vars := values(event.Detail.Additional.Environment); vars[FixtureVersionIdVar] != "" {
		return vars, nil
	}
	if h.CodeBuildApi == nil {
		return nil, fmt.Errorf("fixture build %s has no variables in its event", event.Detail.BuildId)
	}
	out, err := h.CodeBuildApi.BatchGetBuilds(ctx, &codebuild.BatchGetBuildsInput{Ids: []string{event.Detail.BuildId}})
	if err != nil {
		return nil, fmt.Errorf("error getting fixture build %s: %w", event.Detail.BuildId, err)
	}
	if len(out.Builds) == 0 || out.Builds[0].Environment == nil {
		return nil, fmt.Errorf("fixture build %s not found", event.Detail.BuildId)
	}
	env := models.BuildEnvironment{}
	for _, v := range out.Builds[0].Environment.EnvironmentVariables {
		env.EnvironmentVariables = append(env.EnvironmentVariables, models.BuildEnvironmentVariable{Name: aws.ToString(v.Name), Type: string(v.Type), Value: aws.ToString(v.Value)})
	}
	return values(env), nil
}

// readFixtureManifest reads fixtures/<applicationId>/<tag>/manifest.json.
func (h *DeployTaskStateChangeHandler) readFixtureManifest(ctx context.Context, applicationId, tag string) (models.FixturesManifest, error) {
	var m models.FixturesManifest
	key := fixturesKeyPrefix + applicationId + "/" + tag + "/manifest.json"
	out, err := h.Fixtures.S3.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(h.Fixtures.Bucket), Key: aws.String(key)})
	if err != nil {
		return m, fmt.Errorf("reading s3://%s/%s: %w", h.Fixtures.Bucket, key, err)
	}
	defer out.Body.Close()
	body, err := io.ReadAll(io.LimitReader(out.Body, 4<<20))
	if err != nil {
		return m, fmt.Errorf("reading s3://%s/%s: %w", h.Fixtures.Bucket, key, err)
	}
	if err := json.Unmarshal(body, &m); err != nil {
		return m, fmt.Errorf("s3://%s/%s is not a manifest: %w", h.Fixtures.Bucket, key, err)
	}
	return m, nil
}

// setFixtures records the fixtures attribute on the version. Best effort.
func (h *DeployTaskStateChangeHandler) setFixtures(ctx context.Context, logger *slog.Logger, rel release, versionsTable string, rec models.Fixtures) {
	in, err := FixturesUpdate(rel.VersionId, versionsTable, rec)
	if err == nil {
		_, err = h.DynamoDBApi.UpdateItem(ctx, in)
	}
	if err != nil {
		logger.Warn("fixtures: unable to record on the version", slog.String("status", rec.Status), slog.Any("error", err))
	}
}

// FixturesUpdate is the update setFixtures makes.
func FixturesUpdate(versionId, versionsTable string, rec models.Fixtures) (*dynamodb.UpdateItemInput, error) {
	expressions, err := expression.NewBuilder().
		WithCondition(expression.AttributeExists(expression.Name(models.ApplicationKeyField))).
		WithUpdate(expression.Set(expression.Name(models.FixturesField), expression.Value(rec))).
		Build()
	if err != nil {
		return nil, fmt.Errorf("error building the fixtures update of %s: %w", versionId, err)
	}
	return &dynamodb.UpdateItemInput{
		Key:                       models.ApplicationKey(versionId),
		TableName:                 aws.String(versionsTable),
		ConditionExpression:       expressions.Condition(),
		ExpressionAttributeNames:  expressions.Names(),
		ExpressionAttributeValues: expressions.Values(),
		UpdateExpression:          expressions.Update(),
	}, nil
}

// expireSupersededBundles tags the bundles of an app's releases before its
// previous one retain=no; the bucket's lifecycle expires them. The current
// release (the one just built) and the most recent other one stay. Best
// effort.
func (h *DeployTaskStateChangeHandler) expireSupersededBundles(ctx context.Context, logger *slog.Logger, applicationId, currentTag string) {
	fb := h.Fixtures
	prefix := fixturesKeyPrefix + applicationId + "/"
	type bundle struct {
		key      string
		tag      string
		modified time.Time
	}
	var bundles []bundle
	var token *string
	for {
		out, err := fb.S3.ListObjectsV2(ctx, &s3.ListObjectsV2Input{Bucket: aws.String(fb.Bucket), Prefix: aws.String(prefix), ContinuationToken: token})
		if err != nil {
			logger.Warn("fixtures: unable to list the app's bundles; none expired", slog.Any("error", err))
			return
		}
		for _, o := range out.Contents {
			key := aws.ToString(o.Key)
			if !strings.HasSuffix(key, ".tar.gz") {
				continue
			}
			rest := strings.TrimPrefix(key, prefix)
			tag, _, ok := strings.Cut(rest, "/")
			if !ok || tag == "" {
				continue
			}
			b := bundle{key: key, tag: tag}
			if o.LastModified != nil {
				b.modified = *o.LastModified
			}
			bundles = append(bundles, b)
		}
		if out.NextContinuationToken == nil || aws.ToString(out.NextContinuationToken) == "" {
			break
		}
		token = out.NextContinuationToken
	}
	// each tag's newest bundle decides its order
	newest := map[string]time.Time{}
	for _, b := range bundles {
		if b.modified.After(newest[b.tag]) {
			newest[b.tag] = b.modified
		}
	}
	var tags []string
	for t := range newest {
		if t != currentTag {
			tags = append(tags, t)
		}
	}
	sort.Slice(tags, func(i, j int) bool { return newest[tags[i]].After(newest[tags[j]]) })
	keep := map[string]bool{currentTag: true}
	if len(tags) > 0 {
		keep[tags[0]] = true
	}
	for _, b := range bundles {
		if keep[b.tag] {
			continue
		}
		_, err := fb.S3.PutObjectTagging(ctx, &s3.PutObjectTaggingInput{
			Bucket:  aws.String(fb.Bucket),
			Key:     aws.String(b.key),
			Tagging: &s3types.Tagging{TagSet: []s3types.Tag{{Key: aws.String(retainTagKey), Value: aws.String(retainTagNo)}}},
		})
		if err != nil {
			logger.Warn("fixtures: unable to tag a superseded bundle for expiry", slog.String("key", b.key), slog.Any("error", err))
			continue
		}
		logger.Info("fixtures: superseded bundle tagged for expiry", slog.String("key", b.key))
	}
}

// emitReleaseReady sends the App Release Ready event. Best effort.
func (h *DeployTaskStateChangeHandler) emitReleaseReady(ctx context.Context, logger *slog.Logger, rel release, rec models.Fixtures) {
	fb := h.Fixtures
	if fb.Events == nil {
		return
	}
	detail, err := json.Marshal(models.ReleaseReady{
		Environment:   fb.Environment,
		ApplicationId: rel.ApplicationId,
		VersionId:     rel.VersionId,
		Tag:           rel.Tag,
		ImageDigest:   rel.ImageDigest,
		SourceUrl:     rel.SourceUrl,
		Visibility:    rel.Visibility,
		IsPrivate:     rel.IsPrivate,
		Fixtures:      rec,
	})
	if err != nil {
		logger.Warn("fixtures: unable to encode the release event", slog.Any("error", err))
		return
	}
	out, err := fb.Events.PutEvents(ctx, &eventbridge.PutEventsInput{Entries: []ebtypes.PutEventsRequestEntry{{
		Source:     aws.String(models.ReleaseReadySource),
		DetailType: aws.String(models.ReleaseReadyDetailType),
		Detail:     aws.String(string(detail)),
		Resources:  []string{rel.VersionId},
	}}})
	if err == nil && out != nil && out.FailedEntryCount > 0 && len(out.Entries) > 0 {
		err = fmt.Errorf("%s: %s", aws.ToString(out.Entries[0].ErrorCode), aws.ToString(out.Entries[0].ErrorMessage))
	}
	if err != nil {
		logger.Warn("fixtures: unable to send the release event", slog.Any("error", err))
		return
	}
	logger.Info("fixtures: release event sent", slog.String("fixtures", rec.Status))
}

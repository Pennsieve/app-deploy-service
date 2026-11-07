package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"slices"
	"sort"
	"strconv"

	"github.com/pennsieve/app-deploy-service/app-provisioner/provisioner"
	"github.com/pennsieve/app-deploy-service/app-provisioner/provisioner/status"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/pennsieve/app-deploy-service/app-provisioner/provisioner/imagebuild"
	"github.com/pennsieve/app-deploy-service/app-provisioner/provisioner/store_dynamodb"
	"github.com/pennsieve/app-deploy-service/app-provisioner/provisioner/utils"
)

func main() {
	log.Println("Running app Provisioner")
	ctx := context.Background()

	// The provisioner builds App Store images. Installing an app on a compute
	// node (CREATE, DEPLOY, DELETE) is retired: apps run from the App Store.
	action := os.Getenv("ACTION")
	if action != "ADD_TO_APPSTORE" {
		log.Fatalf("action not supported: %s", action)
	}

	applicationUuid := os.Getenv("APPLICATION_UUID")
	sourceUrl := os.Getenv("SOURCE_URL")
	buildStorageGiB := parseBuildStorageGiB(os.Getenv("APP_STORAGE"))
	// APPLICATIONS_TABLE points to the versions table for App Store builds.
	applicationsTable := os.Getenv("APPLICATIONS_TABLE")

	tag := os.Getenv("SOURCE_TAG")
	if tag == "" {
		tag = "latest"
	}

	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		log.Fatalf("LoadDefaultConfig: %v\n", err)
	}
	dynamoDBClient := dynamodb.NewFromConfig(cfg)

	versionStore := store_dynamodb.NewAppStoreVersionDatabaseStore(dynamoDBClient, applicationsTable)
	statusManager := status.NewAppStoreManager(versionStore, applicationUuid)
	deploymentsTable := os.Getenv(provisioner.DeploymentsTableNameKey)
	deploymentId := os.Getenv(provisioner.DeploymentIdKey)
	deploymentsStore := store_dynamodb.NewDeploymentsStore(dynamoDBClient, deploymentsTable)
	statusManager = statusManager.WithDeployment(deploymentsStore, deploymentId)
	// live status updates: AppSync Events where configured, otherwise Pusher
	statusManager = withLiveUpdates(ctx, statusManager, cfg)

	builder, err := newBuildStarter(os.Getenv(imagebuild.BuilderEnv), os.Getenv, cfg)
	if err != nil {
		statusManager.SetErrorStatus(ctx, err)
		log.Fatal(err)
	}
	readVersionBuilds := os.Getenv("READ_VERSION_BUILDS") == "true"
	if err := AddToAppstore(ctx, applicationUuid, deploymentId, sourceUrl, tag, os.Getenv("AUTH_TOKEN"), buildStorageGiB, builder, statusManager, versionStore, deploymentsStore, readVersionBuilds); err != nil {
		statusManager.SetErrorStatus(ctx, err)
		log.Fatal(err)
	}

	log.Println("provisioning complete")
}

func AddToAppstore(ctx context.Context, applicationUuid string, deploymentId string, sourceUrl string, tag string, authToken string, buildStorageGiB int32, builder buildStarter, statusManager *status.Manager, versionStore store_dynamodb.AppStoreVersionDBStore, deployments erroredFlagSetter, readVersionBuilds bool) error {
	// Get the pre-existing private ECR URL from environment variable
	ecrRepoUrl := os.Getenv("APPSTORE_PRIVATE_ECR_URL")
	if ecrRepoUrl == "" {
		return fmt.Errorf("APPSTORE_PRIVATE_ECR_URL environment variable is not set")
	}

	// Generate unique tag using source URL hash: {hash}-{source_tag}
	// This ensures each source gets unique tags in the shared ECR repo
	sourceUrlHash := utils.GenerateHash(sourceUrl)
	uniqueTag := fmt.Sprintf("%d-%s", sourceUrlHash, tag)
	destinationUrl := fmt.Sprintf("%s:%s", ecrRepoUrl, uniqueTag)

	// The version's variants and build settings, when it has any (the
	// service lambda says so with READ_VERSION_BUILDS); otherwise the one
	// image is built as it always was.
	var versionBuilds store_dynamodb.VersionBuilds
	if readVersionBuilds {
		vb, err := versionStore.GetBuilds(ctx, applicationUuid)
		if err != nil {
			return err
		}
		versionBuilds = vb
	}
	builds := planAppStoreBuilds(versionBuilds, deploymentId, buildStorageGiB)
	release := builds[0] // the default variant, or the only image

	log.Printf("Using private ECR repository with unique tag: %s", destinationUrl)

	// Update the version record with destination URL
	if err := versionStore.UpdateDestinationUrl(ctx, applicationUuid, destinationUrl, "deploying"); err != nil {
		return fmt.Errorf("error updating appstore version with destination URL: %w", err)
	}
	if release.Variant != "" {
		statusManager.WithVariant(release.Variant)
		if err := versionStore.UpdateVariant(ctx, applicationUuid, release.Variant, store_dynamodb.VariantUpdate{DestinationUrl: destinationUrl}); err != nil {
			log.Printf("warning: unable to record the image of variant %s: %v", release.Variant, err)
		}
	}
	statusManager.UpdateApplicationStatus(ctx, "deploying", false)

	request := func(b appStoreBuild, destination string) imagebuild.Request {
		r := imagebuild.Request{
			DeploymentId:      b.DeploymentId,
			ApplicationId:     applicationUuid,
			ApplicationsTable: os.Getenv("APPLICATIONS_TABLE"),
			SourceUrl:         sourceUrl,
			Tag:               tag,
			Destination:       destination,
			StorageGiB:        b.StorageGiB,
			Variant:           b.Variant,
			DefaultVariant:    b.Default && b.Variant != "",
		}
		if b.Settings != nil {
			r.BuildArgs, r.Target, r.Dockerfile = b.Settings.Args, b.Settings.Target, b.Settings.Dockerfile
		}
		return r
	}

	// Build and push: the default variant (or the only image) first. Its
	// build not starting fails the version, as it always did; then the other
	// variants aren't built either.
	log.Printf("Initiating new image build (%s): ADD_TO_APPSTORE - sourceUrl: %s, tag: %s, destinationUrl: %s%s", builder.Name(), sourceUrl, tag, destinationUrl, variantLabel(release.Variant))
	if err := builder.Start(ctx, request(release, destinationUrl), authToken); err != nil {
		for _, b := range builds[1:] {
			failVariant(ctx, applicationUuid, b, "not built: the default variant's build did not start", versionStore, deployments, statusManager)
		}
		return err
	}

	// Every other variant: its own image (<hash>-<tag>--<id>), status and
	// deployment. One that doesn't start leaves the version and the other
	// variants as they are.
	for _, b := range builds[1:] {
		destination := imagebuild.VariantDestination(destinationUrl, b.Variant, false)
		if err := versionStore.UpdateVariant(ctx, applicationUuid, b.Variant, store_dynamodb.VariantUpdate{DestinationUrl: destination, Status: "deploying"}); err != nil {
			log.Printf("warning: unable to record the build of variant %s: %v", b.Variant, err)
		}
		statusManager.SendVariantStatusEvent(ctx, b.Variant, b.DeploymentId, "deploying", false)
		log.Printf("Initiating new image build (%s): ADD_TO_APPSTORE - sourceUrl: %s, tag: %s, destinationUrl: %s%s", builder.Name(), sourceUrl, tag, destination, variantLabel(b.Variant))
		if err := builder.Start(ctx, request(b, destination), authToken); err != nil {
			log.Printf("error starting the build of variant %s: %v", b.Variant, err)
			failVariant(ctx, applicationUuid, b, err.Error(), versionStore, deployments, statusManager)
		}
	}
	return nil
}

// erroredFlagSetter marks a deployment as failed.
type erroredFlagSetter interface {
	SetErroredFlag(ctx context.Context, applicationId string, deploymentId string) error
}

// appStoreBuild is one image a release builds.
type appStoreBuild struct {
	Variant      string // "" for an app without variants
	Default      bool   // the version's own image (<hash>-<tag>)
	DeploymentId string
	Settings     *store_dynamodb.BuildSettings
	StorageGiB   int32
}

// planAppStoreBuilds lists the images a release builds, the version's own
// first: the default variant (by the release's deployment) and then each
// other variant the service lambda gave a deployment, in app.yml order. A
// version without variants builds one image, with its top-level build
// settings if any.
func planAppStoreBuilds(vb store_dynamodb.VersionBuilds, releaseDeploymentId string, releaseStorageGiB int32) []appStoreBuild {
	def, ok := vb.Variants[vb.DefaultVariant]
	if vb.DefaultVariant == "" || !ok {
		return []appStoreBuild{{Default: true, DeploymentId: releaseDeploymentId, Settings: vb.Build, StorageGiB: releaseStorageGiB}}
	}
	storage := def.BuildStorageGiB
	if releaseStorageGiB > storage {
		storage = releaseStorageGiB
	}
	builds := []appStoreBuild{{Variant: vb.DefaultVariant, Default: true, DeploymentId: releaseDeploymentId, Settings: def.Build, StorageGiB: storage}}

	order := append([]string(nil), vb.VariantOrder...)
	var rest []string
	for id := range vb.Variants {
		if !slices.Contains(order, id) {
			rest = append(rest, id)
		}
	}
	sort.Strings(rest)
	seen := map[string]bool{vb.DefaultVariant: true}
	for _, id := range append(order, rest...) {
		v, ok := vb.Variants[id]
		if !ok || seen[id] || v.DeploymentId == "" {
			continue
		}
		seen[id] = true
		builds = append(builds, appStoreBuild{Variant: id, DeploymentId: v.DeploymentId, Settings: v.Build, StorageGiB: v.BuildStorageGiB})
	}
	return builds
}

// failVariant records that a non-default variant's build did not start: the
// variant's status and reason, its deployment, and a live event.
func failVariant(ctx context.Context, versionUuid string, b appStoreBuild, reason string, versionStore store_dynamodb.AppStoreVersionDBStore, deployments erroredFlagSetter, statusManager *status.Manager) {
	if err := versionStore.UpdateVariant(ctx, versionUuid, b.Variant, store_dynamodb.VariantUpdate{Status: "error", FailureReason: reason}); err != nil {
		log.Printf("warning: unable to record the failure of variant %s: %v", b.Variant, err)
	}
	if deployments != nil {
		if err := deployments.SetErroredFlag(ctx, versionUuid, b.DeploymentId); err != nil {
			log.Printf("warning: unable to mark the deployment of variant %s as errored: %v", b.Variant, err)
		}
	}
	statusManager.SendVariantStatusEvent(ctx, b.Variant, b.DeploymentId, "error: "+reason, true)
}

func variantLabel(variant string) string {
	if variant == "" {
		return ""
	}
	return ", variant: " + variant
}

// parseBuildStorageGiB parses the APP_STORAGE env var (ephemeral storage in
// GiB) for the build task. It returns 0 (no override) when unset or invalid.
func parseBuildStorageGiB(v string) int32 {
	if v == "" {
		return 0
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		log.Printf("warning: invalid APP_STORAGE value %q, ignoring", v)
		return 0
	}
	return int32(n)
}

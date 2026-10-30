package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/pennsieve/app-deploy-service/app-provisioner/provisioner"
	"github.com/pennsieve/app-deploy-service/app-provisioner/provisioner/status"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	"github.com/aws/aws-sdk-go-v2/service/ecs/types"
	awsProvisioner "github.com/pennsieve/app-deploy-service/app-provisioner/provisioner/aws"
	"github.com/pennsieve/app-deploy-service/app-provisioner/provisioner/imagebuild"
	"github.com/pennsieve/app-deploy-service/app-provisioner/provisioner/parser"
	"github.com/pennsieve/app-deploy-service/app-provisioner/provisioner/runner"
	"github.com/pennsieve/app-deploy-service/app-provisioner/provisioner/store_dynamodb"
	"github.com/pennsieve/app-deploy-service/app-provisioner/provisioner/utils"
)

func main() {
	log.Println("Running app Provisioner")
	ctx := context.Background()

	applicationUuid := os.Getenv("APPLICATION_UUID")
	action := os.Getenv("ACTION")
	accountId := os.Getenv("ACCOUNT_ID")
	accountUuid := os.Getenv("ACCOUNT_UUID")
	env := os.Getenv("ENV")
	sourceUrl := os.Getenv("SOURCE_URL")
	destinationUrl := os.Getenv("DESTINATION_URL")
	storageId := os.Getenv("COMPUTE_NODE_EFS_ID")
	computeNodeUuid := os.Getenv("COMPUTE_NODE_UUID")
	runOnGPU := os.Getenv("RUN_ON_GPU") == "true"
	buildStorageGiB := parseBuildStorageGiB(os.Getenv("APP_STORAGE"))

	applicationsTable := os.Getenv("APPLICATIONS_TABLE")
	accountsTable := os.Getenv("ACCOUNTS_TABLE")

	var tag string
	tag = os.Getenv("SOURCE_TAG")
	if tag == "" {
		tag = "latest"
	}

	// Initializing environment
	cfg, err := config.LoadDefaultConfig(context.Background())
	if err != nil {
		log.Fatalf("LoadDefaultConfig: %v\n", err)
	}

	dynamoDBClient := dynamodb.NewFromConfig(cfg)

	// Look up the account to get the role name (not needed for appstore deployments)
	var roleName, externalId string
	if action != "ADD_TO_APPSTORE" {
		accountStore := store_dynamodb.NewAccountStore(dynamoDBClient, accountsTable)
		account, err := accountStore.GetById(ctx, accountUuid)
		if err != nil {
			log.Fatalf("error looking up account %s: %v", accountUuid, err)
		}
		log.Printf("resolved roleName %q for account %s", account.RoleName, accountUuid)
		roleName = account.RoleName
		externalId = account.ExternalId
	}

	appProvisioner := awsProvisioner.NewAWSProvisioner(cfg,

		accountId, action, env, utils.ExtractGitUrl(sourceUrl), storageId, computeNodeUuid, utils.AppSlug(sourceUrl, computeNodeUuid), runOnGPU, roleName, externalId)
	applicationsStore := store_dynamodb.NewApplicationDatabaseStore(dynamoDBClient, applicationsTable)
	statusManager := status.NewManager(applicationsStore, applicationUuid)

	// deploymentId will only be present if this is not a DELETE or ADD_TO_APPSTORE.
	// ADD_TO_APPSTORE handles its own status manager setup.
	var deploymentId string
	if action == "CREATE" || action == "DEPLOY" {
		deploymentsTable := os.Getenv(provisioner.DeploymentsTableNameKey)
		deploymentId = os.Getenv(provisioner.DeploymentIdKey)
		deploymentsStore := store_dynamodb.NewDeploymentsStore(dynamoDBClient, deploymentsTable)
		statusManager = statusManager.WithDeployment(deploymentsStore, deploymentId)
	}

	// live status updates: AppSync Events where configured, otherwise Pusher
	statusManager = withLiveUpdates(ctx, statusManager, cfg)

	// POST provisioning actions
	switch action {
	case "CREATE":
		ecsClient := ecs.NewFromConfig(cfg)
		if err := Create(ctx, applicationUuid, deploymentId, sourceUrl, appProvisioner, ecsClient, statusManager); err != nil {
			statusManager.SetErrorStatus(ctx, err)
			log.Fatal(err)
		}
	case "DELETE":
		if err := Delete(ctx, applicationUuid, appProvisioner, applicationsStore); err != nil {
			statusManager.UpdateApplicationStatus(ctx, err.Error(), true)
			log.Fatal(err)
		}
	case "DEPLOY":
		// Build and deploy
		ecsClient := ecs.NewFromConfig(cfg)
		if err := Redeploy(ctx, applicationUuid, deploymentId, sourceUrl, destinationUrl, appProvisioner, ecsClient, statusManager); err != nil {
			statusManager.SetErrorStatus(ctx, err)
			log.Fatal(err)
		}
	case "ADD_TO_APPSTORE":
		// APPLICATIONS_TABLE points to the versions table for appstore deployments
		versionStore := store_dynamodb.NewAppStoreVersionDatabaseStore(dynamoDBClient, applicationsTable)
		appStoreStatusManager := status.NewAppStoreManager(versionStore, applicationUuid)
		deploymentsTable := os.Getenv(provisioner.DeploymentsTableNameKey)
		appStoreDeploymentId := os.Getenv(provisioner.DeploymentIdKey)
		deploymentsStore := store_dynamodb.NewDeploymentsStore(dynamoDBClient, deploymentsTable)
		appStoreStatusManager = appStoreStatusManager.WithDeployment(deploymentsStore, appStoreDeploymentId)
		appStoreStatusManager = withLiveUpdates(ctx, appStoreStatusManager, cfg)

		authToken := os.Getenv("AUTH_TOKEN")
		builder, err := newBuildStarter(os.Getenv(imagebuild.BuilderEnv), os.Getenv, cfg, appProvisioner)
		if err != nil {
			appStoreStatusManager.SetErrorStatus(ctx, err)
			log.Fatal(err)
		}
		readVersionBuilds := os.Getenv("READ_VERSION_BUILDS") == "true"
		err = AddToAppstore(ctx, applicationUuid, appStoreDeploymentId, sourceUrl, tag, authToken, buildStorageGiB, builder, appStoreStatusManager, versionStore, deploymentsStore, readVersionBuilds)
		if err != nil {
			appStoreStatusManager.SetErrorStatus(ctx, err)
			log.Fatal(err)
		}
	default:
		unknownActionStatus := fmt.Sprintf("error: unknown provision action: %s", action)
		statusManager.UpdateApplicationStatus(ctx, unknownActionStatus, true)
		log.Fatalf("action not supported: %s", action)
	}

	log.Println("provisioning complete")
}

func Create(ctx context.Context, applicationUuid string, deploymentId string, sourceUrl string, appProvisioner provisioner.Provisioner, ecsClient *ecs.Client, statusManager *status.Manager) error {
	if err := appProvisioner.Create(ctx); err != nil {
		return fmt.Errorf("error creating infrastructure: %w", err)
	}
	// parse output file created after infrastructure creation
	parser := parser.NewOutputParser("/usr/src/app/terraform/infrastructure/outputs.json")
	outputs, err := parser.Run(ctx)
	if err != nil {
		return fmt.Errorf("error running output parser: %w", err)
	}

	// update application record
	store_application := store_dynamodb.Application{
		ApplicationId:            outputs.AppTaskDefn.Value,
		ApplicationContainerName: outputs.AppContainerName.Value,
		DestinationUrl:           outputs.AppEcrUrl.Value,
		Status:                   "deploying",
	}
	err = statusManager.ApplicationCreateUpdate(ctx, store_application)
	if err != nil {
		return fmt.Errorf("error updating application record: %w", err)
	}

	// Build and deploy
	log.Println("Initiating new Deployment Fargate Task: CREATE")
	if err := Deploy(ctx, applicationUuid, deploymentId, sourceUrl, store_application.DestinationUrl, appProvisioner, ecsClient); err != nil {
		return err
	}

	return nil
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

func Redeploy(ctx context.Context, applicationUuid string, deploymentId string, sourceUrl string, destinationUrl string, appProvisioner provisioner.Provisioner, ecsClient *ecs.Client, statusManager *status.Manager) error {
	log.Println("Initiating new Deployment Fargate Task: DEPLOY")
	statusManager.UpdateApplicationStatus(ctx, "re-deploying", false)
	if err := Deploy(ctx, applicationUuid, deploymentId, sourceUrl, destinationUrl, appProvisioner, ecsClient); err != nil {
		return err
	}
	return nil
}

func Deploy(ctx context.Context, applicationUuid string, deploymentId string, sourceUrl string, destinationUrl string, appProvisioner provisioner.Provisioner, ecsClient *ecs.Client) error {
	creds, err := appProvisioner.AssumeRole(ctx)
	if err != nil {
		return fmt.Errorf("error assuming role: %w", err)
	}

	accessKeyId := "AWS_ACCESS_KEY_ID"
	accessKeyIdValue := creds.AccessKeyID
	secretAccessKey := "AWS_SECRET_ACCESS_KEY"
	secretAccessKeyValue := creds.SecretAccessKey
	sessionToken := "AWS_SESSION_TOKEN"
	sessionTokenValue := creds.SessionToken

	TaskDefinitionArn := os.Getenv("DEPLOYER_TASK_DEF_ARN")
	subIdStr := os.Getenv("SUBNET_IDS")
	SubNetIds := strings.Split(subIdStr, ",")
	cluster := os.Getenv("CLUSTER_ARN")
	SecurityGroup := os.Getenv("SECURITY_GROUP")
	TaskDefContainerName := os.Getenv("DEPLOYER_TASK_DEF_CONTAINER_NAME")

	runTaskIn := &ecs.RunTaskInput{
		TaskDefinition: aws.String(TaskDefinitionArn),
		Cluster:        aws.String(cluster),
		NetworkConfiguration: &types.NetworkConfiguration{
			AwsvpcConfiguration: &types.AwsVpcConfiguration{
				Subnets:        SubNetIds,
				SecurityGroups: []string{SecurityGroup},
				AssignPublicIp: types.AssignPublicIpEnabled,
			},
		},
		Overrides: &types.TaskOverride{
			ContainerOverrides: []types.ContainerOverride{
				{
					Name:    &TaskDefContainerName,
					Command: []string{"--context", sourceUrl, "--destination", destinationUrl, "--force"},
					Environment: []types.KeyValuePair{
						{
							Name:  &accessKeyId,
							Value: &accessKeyIdValue,
						},
						{
							Name:  &sessionToken,
							Value: &sessionTokenValue,
						},
						{
							Name:  &secretAccessKey,
							Value: &secretAccessKeyValue,
						},
					},
				},
			},
		},
		LaunchType: types.LaunchTypeFargate,
		Tags: []types.Tag{
			{Key: aws.String(provisioner.DeploymentIdTag), Value: aws.String(deploymentId)},
			{Key: aws.String(provisioner.ApplicationIdTag), Value: aws.String(applicationUuid)},
		},
	}

	taskRunner := runner.NewECSTaskRunner(ecsClient, runTaskIn)
	runTaskOut, err := taskRunner.Run(ctx)
	if err != nil {
		return fmt.Errorf("error running deployment task: %w", err)
	}
	if err := runner.GetRunFailures(runTaskOut); err != nil {
		return fmt.Errorf("error: run failures: %w", err)
	}
	return nil
}

func Delete(ctx context.Context, applicationUuid string, appProvisioner provisioner.Provisioner, applicationsStore store_dynamodb.DynamoDBStore) error {
	log.Println("Deleting", applicationUuid)

	if err := appProvisioner.Delete(ctx); err != nil {
		return fmt.Errorf("error deleting infrastructure: :%w", err)
	}

	if err := applicationsStore.Delete(ctx, applicationUuid); err != nil {
		return fmt.Errorf("error deleting application from store: %w", err)
	}
	return nil
}

func PublicDeploy(ctx context.Context, applicationUuid string, deploymentId string, sourceUrl string, tag string, destinationUrl string, appProvisioner provisioner.Provisioner, ecsClient *ecs.Client) error {
	creds, err := appProvisioner.GetProvisionerCreds(ctx)
	if err != nil {
		return fmt.Errorf("error retrieving credentials: %w", err)
	}

	deploymentSourceUrl, err := utils.DetermineSourceURL(sourceUrl, tag)
	if err != nil {
		return fmt.Errorf("error determining sourceUrl variable for deployment: %w", err)
	}

	accessKeyId := "AWS_ACCESS_KEY_ID"
	accessKeyIdValue := creds.AccessKeyID
	secretAccessKey := "AWS_SECRET_ACCESS_KEY"
	secretAccessKeyValue := creds.SecretAccessKey
	sessionToken := "AWS_SESSION_TOKEN"
	sessionTokenValue := creds.SessionToken

	TaskDefinitionArn := os.Getenv("DEPLOYER_TASK_DEF_ARN")
	subIdStr := os.Getenv("SUBNET_IDS")
	SubNetIds := strings.Split(subIdStr, ",")
	cluster := os.Getenv("CLUSTER_ARN")
	SecurityGroup := os.Getenv("SECURITY_GROUP")
	TaskDefContainerName := os.Getenv("DEPLOYER_TASK_DEF_CONTAINER_NAME")

	runTaskIn := &ecs.RunTaskInput{
		TaskDefinition: aws.String(TaskDefinitionArn),
		Cluster:        aws.String(cluster),
		NetworkConfiguration: &types.NetworkConfiguration{
			AwsvpcConfiguration: &types.AwsVpcConfiguration{
				Subnets:        SubNetIds,
				SecurityGroups: []string{SecurityGroup},
				AssignPublicIp: types.AssignPublicIpEnabled,
			},
		},
		Overrides: &types.TaskOverride{
			ContainerOverrides: []types.ContainerOverride{
				{
					Name:    &TaskDefContainerName,
					Command: []string{"--context", deploymentSourceUrl, "--destination", fmt.Sprintf("%s:%s", destinationUrl, tag), "--force"},
					Environment: []types.KeyValuePair{
						{
							Name:  &accessKeyId,
							Value: &accessKeyIdValue,
						},
						{
							Name:  &sessionToken,
							Value: &sessionTokenValue,
						},
						{
							Name:  &secretAccessKey,
							Value: &secretAccessKeyValue,
						},
					},
				},
			},
		},
		LaunchType: types.LaunchTypeFargate,
		Tags: []types.Tag{
			{Key: aws.String(provisioner.DeploymentIdTag), Value: aws.String(deploymentId)},
			{Key: aws.String(provisioner.ApplicationIdTag), Value: aws.String(applicationUuid)},
		},
	}

	taskRunner := runner.NewECSTaskRunner(ecsClient, runTaskIn)
	runTaskOut, err := taskRunner.Run(ctx)
	if err != nil {
		return fmt.Errorf("error running deployment task: %w", err)
	}
	if err := runner.GetRunFailures(runTaskOut); err != nil {
		return fmt.Errorf("error: run failures: %w", err)
	}
	return nil
}

// PrivateDeploy starts the kaniko task that builds an App Store image
// (imagebuild.Request) and pushes it to r.Destination.
func PrivateDeploy(ctx context.Context, r imagebuild.Request, authToken string, appProvisioner provisioner.Provisioner, ecsClient *ecs.Client) error {
	creds, err := appProvisioner.GetProvisionerCreds(ctx)
	if err != nil {
		return fmt.Errorf("error retrieving credentials: %w", err)
	}

	deploymentSourceUrl, err := utils.DetermineSourceURL(r.SourceUrl, r.Tag)
	if err != nil {
		return fmt.Errorf("error determining sourceUrl variable for deployment: %w", err)
	}

	envVars := []types.KeyValuePair{
		{Name: aws.String("AWS_ACCESS_KEY_ID"), Value: aws.String(creds.AccessKeyID)},
		{Name: aws.String("AWS_SESSION_TOKEN"), Value: aws.String(creds.SessionToken)},
		{Name: aws.String("AWS_SECRET_ACCESS_KEY"), Value: aws.String(creds.SecretAccessKey)},
	}
	// Add GIT_TOKEN for kaniko to authenticate with private GitHub repos
	if authToken != "" {
		envVars = append(envVars, types.KeyValuePair{
			Name:  aws.String("GIT_TOKEN"),
			Value: aws.String(authToken),
		})
	}

	if r.StorageGiB > 0 {
		log.Printf("overriding build ephemeral storage to %d GiB", r.StorageGiB)
	}
	runTaskIn := kanikoRunTaskInput(r, deploymentSourceUrl, envVars, os.Getenv)

	taskRunner := runner.NewECSTaskRunner(ecsClient, runTaskIn)
	runTaskOut, err := taskRunner.Run(ctx)
	if err != nil {
		return fmt.Errorf("error running deployment task: %w", err)
	}
	if err := runner.GetRunFailures(runTaskOut); err != nil {
		return fmt.Errorf("error: run failures: %w", err)
	}
	return nil
}

// kanikoRunTaskInput is the kaniko task for an App Store image build. The
// destination already holds the full image reference with its unique tag:
// {ecr_repo}:{source_hash}-{source_tag}[--{variant}]. Build settings become
// kaniko flags (imagebuild.KanikoFlags), and a variant's build is tagged
// with Variant and VariantDefault for the status lambda. Without variants
// or build settings, the task is the one it always was.
func kanikoRunTaskInput(r imagebuild.Request, deploymentSourceUrl string, envVars []types.KeyValuePair, getenv func(string) string) *ecs.RunTaskInput {
	command := append([]string{"--context", deploymentSourceUrl, "--destination", r.Destination, "--force"}, imagebuild.KanikoFlags(r)...)
	runTaskIn := &ecs.RunTaskInput{
		TaskDefinition: aws.String(getenv("DEPLOYER_TASK_DEF_ARN")),
		Cluster:        aws.String(getenv("CLUSTER_ARN")),
		NetworkConfiguration: &types.NetworkConfiguration{
			AwsvpcConfiguration: &types.AwsVpcConfiguration{
				Subnets:        strings.Split(getenv("SUBNET_IDS"), ","),
				SecurityGroups: []string{getenv("SECURITY_GROUP")},
				AssignPublicIp: types.AssignPublicIpEnabled,
			},
		},
		Overrides: &types.TaskOverride{
			ContainerOverrides: []types.ContainerOverride{
				{
					Name:        aws.String(getenv("DEPLOYER_TASK_DEF_CONTAINER_NAME")),
					Command:     command,
					Environment: envVars,
				},
			},
		},
		LaunchType: types.LaunchTypeFargate,
		Tags: []types.Tag{
			{Key: aws.String(provisioner.DeploymentIdTag), Value: aws.String(r.DeploymentId)},
			{Key: aws.String(provisioner.ApplicationIdTag), Value: aws.String(r.ApplicationId)},
		},
	}

	// Propagate ApplicationsTable tag so the status lambda updates the correct table
	if r.ApplicationsTable != "" {
		runTaskIn.Tags = append(runTaskIn.Tags, types.Tag{
			Key:   aws.String(provisioner.ApplicationsTableTag),
			Value: aws.String(r.ApplicationsTable),
		})
	}
	if r.Variant != "" {
		runTaskIn.Tags = append(runTaskIn.Tags,
			types.Tag{Key: aws.String(provisioner.VariantTag), Value: aws.String(r.Variant)},
			types.Tag{Key: aws.String(provisioner.VariantDefaultTag), Value: aws.String(strconv.FormatBool(r.DefaultVariant))})
	}

	// Override the build task's ephemeral storage for apps that need more space
	// than Fargate's default (e.g. GPU apps with large dependencies).
	if r.StorageGiB > 0 {
		runTaskIn.Overrides.EphemeralStorage = &types.EphemeralStorage{
			SizeInGiB: r.StorageGiB,
		}
	}
	return runTaskIn
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

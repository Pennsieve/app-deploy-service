// backfill-manifests stores the app.yml manifest of every app version
// registered before versions kept theirs, from the content-sync bucket, and
// sets each app's params from its newest version. Run once per environment
// after deploying, with AWS credentials for it:
//
//	APPSTORE_APPLICATIONS_TABLE=... APPSTORE_VERSIONS_TABLE=... CONTENT_SYNC_BUCKET=... \
//	  go run ./cmd/backfill-manifests [-dry-run]
package main

import (
	"context"
	"flag"
	"log"
	"os"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/pennsieve/app-deploy-service/service/handler"
	"github.com/pennsieve/app-deploy-service/service/store_dynamodb"
)

func main() {
	dryRun := flag.Bool("dry-run", false, "report what would be stored, without writing")
	flag.Parse()
	for _, v := range []string{"APPSTORE_APPLICATIONS_TABLE", "APPSTORE_VERSIONS_TABLE", "CONTENT_SYNC_BUCKET"} {
		if os.Getenv(v) == "" {
			log.Fatalf("set %s", v)
		}
	}
	ctx := context.Background()
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		log.Fatal(err)
	}
	client := dynamodb.NewFromConfig(cfg)
	res, err := handler.BackfillManifests(ctx,
		store_dynamodb.NewAppStoreDatabaseStore(client, os.Getenv("APPSTORE_APPLICATIONS_TABLE")),
		store_dynamodb.NewAppStoreVersionDatabaseStore(client, os.Getenv("APPSTORE_VERSIONS_TABLE")),
		handler.SyncedManifestReader(cfg), *dryRun, log.Printf)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("%d apps, %d versions: %d manifests stored, %d versions without app.yml, %d apps' params set (dry run: %v)",
		res.Apps, res.Versions, res.Stored, res.WithoutAppYml, res.ParamsSet, *dryRun)
}

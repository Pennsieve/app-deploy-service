// backfill-lambda-eligibility decides and records whether each App Store
// version (and variant) built before the status lambda recorded it can run
// as a Lambda step (the version's `lambda`, docs/builds.md), from its stored
// manifest and its image in ECR. Run once per environment after deploying,
// after backfill-manifests (lambda/service) when that is still to run, with
// AWS credentials for it:
//
//	APPSTORE_VERSIONS_TABLE=... go run ./cmd/backfill-lambda-eligibility [-dry-run]
//
// It is safe to run again: each run decides afresh and overwrites lambda.
package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/ecr"
	"github.com/pennsieve/app-deploy-service/status/handler"
)

func main() {
	dryRun := flag.Bool("dry-run", false, "print what would be recorded, without writing")
	flag.Parse()
	table := os.Getenv("APPSTORE_VERSIONS_TABLE")
	if table == "" {
		log.Fatal("set APPSTORE_VERSIONS_TABLE")
	}
	ctx := context.Background()
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		log.Fatal(err)
	}
	res, err := handler.BackfillLambdaEligibility(ctx, dynamodb.NewFromConfig(cfg), ecr.NewFromConfig(cfg),
		&http.Client{Timeout: 30 * time.Second}, table, *dryRun, time.Now, log.Printf)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("%d versions, %d variants: %d eligible, %d not, %d images unreadable (dry run: %v)",
		res.Versions, res.Variants, res.Eligible, res.Ineligible, res.Unreadable, *dryRun)
}

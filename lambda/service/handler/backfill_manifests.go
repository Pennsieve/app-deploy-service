package handler

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/pennsieve/app-deploy-service/service/store_dynamodb"
)

// storedManifest is what registration stores from a version's app.yml: its
// manifest (the default variant's, with variants) and its variants.
type storedManifest struct {
	Manifest *store_dynamodb.AppManifest
	Variants store_dynamodb.VersionVariants
}

// storedManifestOf resolves an app.yml as registration does.
func storedManifestOf(c appConfig) storedManifest {
	r := resolveVariants(c)
	return storedManifest{Manifest: r.versionManifest(), Variants: r.versionVariants()}
}

// manifestReader reads a version's synced app.yml: false when it has none.
type manifestReader func(ctx context.Context, sourceUrl, tag string) (storedManifest, bool)

// SyncedManifestReader reads manifests from the content-sync bucket and
// resolves their variants, as registration does.
func SyncedManifestReader(cfg aws.Config) manifestReader {
	return func(ctx context.Context, sourceUrl, tag string) (storedManifest, bool) {
		c, ok := readSyncedAppConfig(ctx, cfg, sourceUrl, tag)
		if !ok {
			return storedManifest{}, false
		}
		return storedManifestOf(c), true
	}
}

// BackfillResult counts what a backfill did.
type BackfillResult struct {
	Apps, Versions, Stored, WithoutAppYml, ParamsSet int
}

// BackfillManifests stores the manifest of every version registered before
// manifests were kept, from its synced app.yml resolved as registration
// does (with variants: the default's manifest, plus the variants), and sets
// each app's top-level params from its newest version's (undoing any
// rollback by a re-registered older tag). Versions without an app.yml are
// left without a manifest. With dryRun nothing is written.
func BackfillManifests(ctx context.Context, apps store_dynamodb.AppStoreDBStore, versions store_dynamodb.AppStoreVersionDBStore,
	read manifestReader, dryRun bool, logf func(string, ...any)) (BackfillResult, error) {
	var res BackfillResult
	all, err := apps.GetAll(ctx)
	if err != nil {
		return res, fmt.Errorf("listing apps: %w", err)
	}
	for _, app := range all {
		res.Apps++
		vs, err := versions.GetByApplicationId(ctx, app.Uuid)
		if err != nil {
			return res, fmt.Errorf("listing the versions of %s: %w", app.Uuid, err)
		}
		tagged := make([]taggedVersion, 0, len(vs))
		manifests := map[string]*store_dynamodb.AppManifest{}
		for _, v := range vs {
			res.Versions++
			tagged = append(tagged, taggedVersion{Tag: v.Version, CreatedAt: v.CreatedAt})
			m := v.Manifest
			if m == nil && v.Version != "" {
				stored, ok := read(ctx, app.SourceUrl, v.Version)
				if !ok {
					res.WithoutAppYml++
					continue
				}
				m = stored.Manifest
				logf("%s %s: storing its manifest", app.SourceUrl, v.Version)
				if !dryRun {
					if err := versions.SetManifest(ctx, v.Uuid, m); err != nil {
						return res, err
					}
					if !stored.Variants.Empty() {
						if err := versions.SetVariants(ctx, v.Uuid, stored.Variants); err != nil {
							return res, err
						}
					}
				}
				res.Stored++
			}
			if m != nil {
				manifests[v.Version] = m
			}
		}
		newest := newestTag(tagged)
		if m, ok := manifests[newest]; ok {
			logf("%s: params from its newest version, %s", app.SourceUrl, newest)
			if !dryRun {
				if err := apps.UpdateParams(ctx, app.Uuid, m.Parameters); err != nil {
					return res, err
				}
			}
			res.ParamsSet++
		}
	}
	return res, nil
}

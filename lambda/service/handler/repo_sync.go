package handler

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	github "github.com/pennsieve/github-client/pkg/github"
	ghsync "github.com/pennsieve/github-client/pkg/github/sync"
)

var syncFiles = []string{"pennsieve.json", "README.md"}

func newGitHubClient(token string) github.GitHubApi {
	client := github.NewGitHubApiClient(logger, "", "", github.GitHubApiUrl, 0)
	if token != "" {
		client = client.WithAccessToken(token)
	}
	return client
}

type gitHubContentFetcher struct {
	client github.GitHubApi
}

func (f *gitHubContentFetcher) GetContent(url, filePath, tag string) (*ghsync.ContentResponse, error) {
	resp, err := f.client.GetContent(url, filePath, tag)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, nil
	}
	return &ghsync.ContentResponse{
		Content:  resp.Content,
		Encoding: resp.Encoding,
	}, nil
}

func buildNamespace(sourceUrl string, tag string) string {
	sourceUrl = strings.TrimSuffix(sourceUrl, "/")
	sourceUrl = strings.TrimSuffix(sourceUrl, ".git")
	parts := strings.Split(sourceUrl, "/")
	if len(parts) >= 2 {
		owner := parts[len(parts)-2]
		repo := parts[len(parts)-1]
		return fmt.Sprintf("%s/%s/%s", owner, repo, tag)
	}
	return tag
}

func syncRepoContent(ctx context.Context, sourceUrl string, tag string, authToken string) {
	if tag == "" {
		tag = "main"
	}

	bucket := os.Getenv("CONTENT_SYNC_BUCKET")
	if bucket == "" {
		log.Println("warning: CONTENT_SYNC_BUCKET not set, skipping S3 sync")
		return
	}

	ghClient := newGitHubClient(authToken)
	fetcher := &gitHubContentFetcher{client: ghClient}

	cfg, err := awsconfig.LoadDefaultConfig(ctx)
	if err != nil {
		log.Printf("warning: failed to load AWS config for S3 sync: %v", err)
		return
	}
	s3Client := s3.NewFromConfig(cfg)
	dest := ghsync.NewS3Destination(s3Client, bucket)

	namespace := buildNamespace(sourceUrl, tag)

	config := ghsync.Config{
		RepoUrl:   sourceUrl,
		Tag:       tag,
		Namespace: namespace,
		Files:     syncFiles,
	}

	results := ghsync.SyncContent(ctx, logger, fetcher, config, dest)
	for _, r := range results {
		if r.Error != nil {
			log.Printf("warning: sync failed for %s: %v", r.File, r.Error)
		}
	}
}

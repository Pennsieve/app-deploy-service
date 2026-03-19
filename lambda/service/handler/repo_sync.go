package handler

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"mime"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	github "github.com/pennsieve/github-client/pkg/github"
)

var syncFiles = []string{"pennsieve.json", "README.md"}

func newGitHubClient(token string) github.GitHubApi {
	client := github.NewGitHubApiClient(logger, "", "", github.GitHubApiUrl, 0)
	if token != "" {
		client = client.WithAccessToken(token)
	}
	return client
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

	cfg, err := awsconfig.LoadDefaultConfig(ctx)
	if err != nil {
		log.Printf("warning: failed to load AWS config for S3 sync: %v", err)
		return
	}
	s3Client := s3.NewFromConfig(cfg)

	namespace := buildNamespace(sourceUrl, tag)
	var wg sync.WaitGroup

	for _, file := range syncFiles {
		wg.Add(1)
		go func(filePath string) {
			defer wg.Done()

			content, err := ghClient.GetFileContent(sourceUrl, filePath, tag)
			if err != nil {
				log.Printf("warning: failed to fetch %s: %v", filePath, err)
				return
			}
			if content == nil {
				log.Printf("warning: %s not found in repo", filePath)
				return
			}

			key := fmt.Sprintf("%s/%s", namespace, filePath)
			contentType := detectContentType(filePath)

			_, err = s3Client.PutObject(ctx, &s3.PutObjectInput{
				Bucket:      aws.String(bucket),
				Key:         aws.String(key),
				Body:        bytes.NewReader(content),
				ContentType: aws.String(contentType),
			})
			if err != nil {
				log.Printf("warning: failed to sync %s to S3: %v", filePath, err)
				return
			}

			logger.Info(fmt.Sprintf("synced %s to s3://%s/%s", filePath, bucket, key))
		}(file)
	}

	wg.Wait()
}

func detectContentType(filePath string) string {
	ext := filepath.Ext(filePath)
	if ct := mime.TypeByExtension(ext); ct != "" {
		return ct
	}
	return "application/octet-stream"
}

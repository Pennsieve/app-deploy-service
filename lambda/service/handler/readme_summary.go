package handler

import (
	"context"
	"log"
	"os"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	ghsync "github.com/pennsieve/github-client/pkg/github/sync"
)

// summaryMaxChars bounds an app's README summary: a card's worth of text.
const summaryMaxChars = 220

var (
	mdFence    = regexp.MustCompile("(?s)```.*?```")
	mdImage    = regexp.MustCompile(`!\[[^\]]*\]\([^)]*\)`)
	mdLink     = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)
	mdRefLink  = regexp.MustCompile(`\[([^\]]*)\]\[[^\]]*\]`)
	mdHTML     = regexp.MustCompile(`<[^>]+>`)
	mdHeading  = regexp.MustCompile(`^#{1,6}\s`)
	mdEmphasis = regexp.MustCompile("[*_`~>]")
	mdSpaces   = regexp.MustCompile(`\s+`)
)

// readmeSummary is the first real paragraph of a README as plain text, cut at
// a word boundary to about summaryMaxChars: what an app's card shows when its
// app.yml has no description. Plain text extraction (no model): headings,
// badge and image lines, code blocks and HTML are skipped.
func readmeSummary(md string) string {
	text := mdFence.ReplaceAllString(md, "\n\n")
	// Paragraphs: blank lines and headings end one.
	var paragraphs [][]string
	var current []string
	flush := func() {
		if len(current) > 0 {
			paragraphs = append(paragraphs, current)
			current = nil
		}
	}
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || mdHeading.MatchString(line) || strings.HasPrefix(line, "---") || strings.HasPrefix(line, "===") {
			flush()
			continue
		}
		current = append(current, line)
	}
	flush()
	// The first with text left once markup is gone (badges and images
	// alone leave none).
	for _, p := range paragraphs {
		if s := plainText(strings.Join(p, " ")); s != "" {
			return truncateAtWord(s, summaryMaxChars)
		}
	}
	return ""
}

func plainText(s string) string {
	s = mdImage.ReplaceAllString(s, "")
	s = mdLink.ReplaceAllString(s, "$1")
	s = mdRefLink.ReplaceAllString(s, "$1")
	s = mdHTML.ReplaceAllString(s, "")
	s = mdEmphasis.ReplaceAllString(s, "")
	return strings.TrimSpace(mdSpaces.ReplaceAllString(s, " "))
}

func truncateAtWord(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	cut := string([]rune(s)[:max])
	if i := strings.LastIndex(cut, " "); i > max/2 {
		cut = cut[:i]
	}
	return strings.TrimRight(cut, " ,;:.-") + "…"
}

// syncedReadmeSummary reads the README synced for a release (CONTENT_SYNC_
// BUCKET) and summarizes it; "" when there is none.
func syncedReadmeSummary(ctx context.Context, cfg aws.Config, sourceUrl, tag string) string {
	bucket := os.Getenv("CONTENT_SYNC_BUCKET")
	if bucket == "" {
		return ""
	}
	if tag == "" {
		tag = "main"
	}
	dest := ghsync.NewS3Destination(s3.NewFromConfig(cfg), bucket)
	data, _, err := dest.Read(ctx, buildNamespace(sourceUrl, tag)+"/README.md")
	if err != nil {
		log.Printf("no README to summarize for %s %s: %v", sourceUrl, tag, err)
		return ""
	}
	return readmeSummary(string(data))
}

package handler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ecr"
	ecrtypes "github.com/aws/aws-sdk-go-v2/service/ecr/types"
	"github.com/pennsieve/app-deploy-service/status/models"
)

// Promotion of a staged image (docs/builds.md "Isolation"). With a staging
// repository, a build pushes <staging>:<deploymentId> (DESTINATION) and
// names its App Store image in PROMOTE_TO. The build's role can only write
// to staging; this lambda, platform code, copies the image to its
// destination after the build SUCCEEDED and then deletes the staging tag.
// A promotion that fails fails the build (the version becomes error with
// the reason), so a deployed version always names a promoted image.

const (
	buildDestinationVar = "DESTINATION"
	buildPromoteToVar   = "PROMOTE_TO"
	// StopCodePromoteFailed marks a build whose image could not be promoted.
	StopCodePromoteFailed = "PROMOTE_FAILED"
)

// ImageCopier copies an image reference to another in the same registry,
// manifest digest preserved.
type ImageCopier interface {
	Copy(ctx context.Context, src, dst string) error
}

// WithImageCopier lets the handler promote staged images.
func (h *DeployTaskStateChangeHandler) WithImageCopier(c ImageCopier) *DeployTaskStateChangeHandler {
	h.ImageCopier = c
	return h
}

// promotion is what a build's variables say about its staging.
type promotion struct {
	Staged string // DESTINATION, the staging image
	Target string // PROMOTE_TO, the App Store image
}

func promotionOf(env models.BuildEnvironment) (promotion, bool) {
	target, ok := env.Variable(buildPromoteToVar)
	if !ok || strings.TrimSpace(target.Value) == "" {
		return promotion{}, false
	}
	staged, ok := env.Variable(buildDestinationVar)
	if !ok || strings.TrimSpace(staged.Value) == "" {
		return promotion{}, false
	}
	return promotion{Staged: strings.TrimSpace(staged.Value), Target: strings.TrimSpace(target.Value)}, true
}

var errNoImageCopier = errors.New("the status lambda has no image copier configured")

// promote copies the staged image to its App Store destination and deletes
// the staging tag (best effort; the staging repository's lifecycle expires
// what is left). Returns the reason when promotion failed.
func (h *DeployTaskStateChangeHandler) promote(ctx context.Context, logger *slog.Logger, p promotion) error {
	if h.ImageCopier == nil {
		return errNoImageCopier
	}
	logger = logger.With(slog.String("staged", p.Staged), slog.String("target", p.Target))
	logger.Info("promoting the staged image")
	if err := h.ImageCopier.Copy(ctx, p.Staged, p.Target); err != nil {
		return fmt.Errorf("copying %s to %s: %w", p.Staged, p.Target, err)
	}
	logger.Info("promoted the staged image")
	h.deleteStagedImage(ctx, logger, p.Staged)
	return nil
}

// deleteStagedImage removes the staging tag after promotion. Best effort.
func (h *DeployTaskStateChangeHandler) deleteStagedImage(ctx context.Context, logger *slog.Logger, image string) {
	if h.ECRApi == nil {
		return
	}
	registry, repo, tag, ok := splitImage(image)
	if !ok || !strings.Contains(registry, ".dkr.ecr.") {
		logger.Warn("not deleting the staged image: not an ECR image")
		return
	}
	_, err := h.ECRApi.BatchDeleteImage(ctx, &ecr.BatchDeleteImageInput{
		RepositoryName: aws.String(repo),
		ImageIds:       []ecrtypes.ImageIdentifier{{ImageTag: aws.String(tag)}},
	})
	if err != nil {
		logger.Warn("could not delete the staged image; its repository's lifecycle will", slog.String("error", err.Error()))
		return
	}
	logger.Info("deleted the staged image")
}

// splitImage splits <registry>/<repository>:<tag>.
func splitImage(image string) (registry, repo, tag string, ok bool) {
	slash := strings.Index(image, "/")
	colon := strings.LastIndex(image, ":")
	if slash < 0 || colon < slash {
		return "", "", "", false
	}
	return image[:slash], image[slash+1 : colon], image[colon+1:], true
}

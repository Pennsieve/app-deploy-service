// Package imagecopy copies an image between repositories of the account's
// ECR registry with go-containerregistry: the manifest and its layers, the
// digest preserved. Layers the registry can mount across repositories are
// mounted; the rest are streamed, never held in memory whole.
package imagecopy

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ecr"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
)

// ECRAuthAPI mints registry credentials.
type ECRAuthAPI interface {
	GetAuthorizationToken(ctx context.Context, params *ecr.GetAuthorizationTokenInput, optFns ...func(*ecr.Options)) (*ecr.GetAuthorizationTokenOutput, error)
}

// Copier copies images with credentials from ECR, refreshed before they
// expire (ECR tokens last 12 hours).
type Copier struct {
	ECR ECRAuthAPI

	mu      sync.Mutex
	auth    *authn.Basic
	expires time.Time
	now     func() time.Time
}

func New(ecrApi ECRAuthAPI) *Copier {
	return &Copier{ECR: ecrApi, now: time.Now}
}

func (c *Copier) authenticator(ctx context.Context) (authn.Authenticator, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.auth != nil && c.now().Add(10*time.Minute).Before(c.expires) {
		return c.auth, nil
	}
	out, err := c.ECR.GetAuthorizationToken(ctx, &ecr.GetAuthorizationTokenInput{})
	if err != nil {
		return nil, fmt.Errorf("ECR authorization token: %w", err)
	}
	if len(out.AuthorizationData) == 0 {
		return nil, fmt.Errorf("ECR returned no authorization data")
	}
	data := out.AuthorizationData[0]
	raw, err := base64.StdEncoding.DecodeString(aws.ToString(data.AuthorizationToken))
	if err != nil {
		return nil, fmt.Errorf("ECR authorization token is not base64: %w", err)
	}
	user, pass, ok := strings.Cut(string(raw), ":")
	if !ok {
		return nil, fmt.Errorf("ECR authorization token has no user:password")
	}
	c.auth = &authn.Basic{Username: user, Password: pass}
	if data.ExpiresAt != nil {
		c.expires = *data.ExpiresAt
	} else {
		c.expires = c.now().Add(time.Hour)
	}
	return c.auth, nil
}

// Copy copies src to dst (both <registry>/<repository>:<tag>).
func (c *Copier) Copy(ctx context.Context, src, dst string) error {
	srcRef, err := name.ParseReference(src)
	if err != nil {
		return fmt.Errorf("source %q: %w", src, err)
	}
	dstRef, err := name.ParseReference(dst)
	if err != nil {
		return fmt.Errorf("destination %q: %w", dst, err)
	}
	auth, err := c.authenticator(ctx)
	if err != nil {
		return err
	}
	opts := []remote.Option{remote.WithAuth(auth), remote.WithContext(ctx)}
	desc, err := remote.Get(srcRef, opts...)
	if err != nil {
		return fmt.Errorf("reading %s: %w", src, err)
	}
	// An index (several platforms) or a single image: write whichever it is,
	// so the destination's digest equals the source's.
	if desc.MediaType.IsIndex() {
		idx, err := desc.ImageIndex()
		if err != nil {
			return fmt.Errorf("reading index %s: %w", src, err)
		}
		if err := remote.WriteIndex(dstRef, idx, opts...); err != nil {
			return fmt.Errorf("writing %s: %w", dst, err)
		}
		return nil
	}
	img, err := desc.Image()
	if err != nil {
		return fmt.Errorf("reading image %s: %w", src, err)
	}
	if err := remote.Write(dstRef, img, opts...); err != nil {
		return fmt.Errorf("writing %s: %w", dst, err)
	}
	return nil
}

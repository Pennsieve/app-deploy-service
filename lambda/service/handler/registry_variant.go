package handler

import (
	"fmt"
	"net/http"

	"github.com/pennsieve/app-deploy-service/service/store_dynamodb"
)

// Registration statuses of a version, and of each of its variants.
const (
	registrationDeployed = "deployed"
	registrationError    = "error"
)

// variantTagSeparator joins a release tag and a non-default variant id in
// the image tag: <hash>-<tag>--<id>. Release tags of apps with variants may
// not contain it, and variant ids can't.
const variantTagSeparator = "--"

// registryRefusal is why the registry won't hand out a version's image: the
// HTTP status and the message for the caller.
type registryRefusal struct {
	Status  int
	Message string
}

// registryDestination is the image the registry authorizes for a version and
// a variant ("" is the default), or why it refuses.
//
//   - No variant: the version's own destinationUrl and status, which are the
//     default variant's (today's behaviour, for every app).
//   - A variant on a version without variants: refused.
//   - A variant the version doesn't have: refused.
//   - A variant: its own status and image. A non-default variant without a
//     status has no build recorded and counts as not yet deployed; the
//     default variant without one follows the version, which is its status.
//     Without its own destinationUrl, the default variant's image is the
//     version's and another's is the version's with "--<id>" appended.
func registryDestination(ver store_dynamodb.AppStoreVersion, variant string) (string, *registryRefusal) {
	if variant == "" {
		if ver.DestinationUrl == "" || ver.Status != registrationDeployed {
			return "", &registryRefusal{http.StatusNotFound, "version is not yet deployed"}
		}
		return ver.DestinationUrl, nil
	}

	if len(ver.Variants) == 0 {
		return "", &registryRefusal{http.StatusNotFound, fmt.Sprintf("version %s has no variants", ver.Version)}
	}
	v, ok := ver.Variants[variant]
	if !ok {
		return "", &registryRefusal{http.StatusNotFound, fmt.Sprintf("variant %s not found for this version", variant)}
	}

	isDefault := variant == ver.DefaultVariant
	status, url := v.Status, v.DestinationUrl
	if isDefault {
		if status == "" {
			status = ver.Status
		}
		if url == "" {
			url = ver.DestinationUrl
		}
	} else if url == "" && ver.DestinationUrl != "" {
		url = ver.DestinationUrl + variantTagSeparator + variant
	}

	switch {
	case status == registrationError:
		return "", &registryRefusal{http.StatusNotFound, fmt.Sprintf("variant %s of %s failed to build", variant, ver.Version)}
	case status != registrationDeployed || url == "":
		return "", &registryRefusal{http.StatusNotFound, fmt.Sprintf("variant %s of %s is not yet deployed", variant, ver.Version)}
	}
	return url, nil
}

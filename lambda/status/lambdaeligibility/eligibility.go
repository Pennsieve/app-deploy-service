// Package lambdaeligibility decides whether an App Store version (or one of
// its variants) can run as a Lambda step. Every image the App Store builds
// carries the Lambda step runtime, so authors don't opt in: the platform
// decides from facts, the version's runtime (app.yml) and its image, and
// records the decision on the version as `lambda` (docs/builds.md). The
// limits mirror what compute-node-aws-provisioner checks when it creates a
// step's function (internal/aslconverter/lambda_image.go).
package lambdaeligibility

import (
	"fmt"
	"strings"
	"time"
)

const (
	// ImageLabel is the label the image builder sets when it appends the
	// Lambda step runtime to an image (terraform/image-builder/build.sh).
	ImageLabel = "net.pennsieve.lambda-step-runtime"

	// MaxImageBytes is the largest image Lambda deploys (10 GB, decimal),
	// compared with the image's compressed size, a lower bound on its
	// unpacked one.
	MaxImageBytes = 10_000_000_000
	// MaxMemoryMiB is a Lambda function's most memory.
	MaxMemoryMiB = 10240
	// MaxCPUUnits is the most CPU a Lambda function gets: 6 vCPU, in the
	// manifest's units (1024 = 1 vCPU).
	MaxCPUUnits = 6144
	// MaxTimeoutSeconds is the longest timeoutSeconds that fits Lambda:
	// Lambda's own 15 minutes, which authors write for a Lambda-sized app.
	// It is an upper bound, not a run time; the compute node still stops a
	// Lambda step at 840 s (lambdaStepTimeoutSeconds, a minute under the
	// step runner's 900 s), as before.
	MaxTimeoutSeconds = 900
)

// Eligibility is the decision, as stored on a version and on each variant
// (attribute `lambda`). Reasons are why it isn't eligible, all that apply;
// ImageBytes is the image's compressed size (the sum of its layers);
// CheckedAt is when it was decided, RFC 3339.
type Eligibility struct {
	Eligible   bool     `dynamodbav:"eligible" json:"eligible"`
	Reasons    []string `dynamodbav:"reasons,omitempty" json:"reasons,omitempty"`
	ImageBytes int64    `dynamodbav:"imageBytes,omitempty" json:"imageBytes,omitempty"`
	CheckedAt  string   `dynamodbav:"checkedAt" json:"checkedAt"`
}

// Runtime is what eligibility reads of a stored manifest's runtime
// (app-deploy-service's store_dynamodb.AppRuntime). Zero means undeclared.
type Runtime struct {
	CPU            int      `dynamodbav:"cpu,omitempty"`
	Memory         int      `dynamodbav:"memory,omitempty"`
	ComputeTypes   []string `dynamodbav:"computeTypes,omitempty"`
	TimeoutSeconds int      `dynamodbav:"timeoutSeconds,omitempty"`
	Interactive    bool     `dynamodbav:"interactive,omitempty"`
	SessionType    string   `dynamodbav:"sessionType,omitempty"`
	// Optimize is app.yml's runtime.optimize: false runs the app only as
	// computeTypes declares it (no Lambda); nil is the default (true).
	Optimize *bool `dynamodbav:"optimize,omitempty"`
}

// Manifest is what eligibility reads of a stored manifest: its runtime and
// whether any input is filled from the reference catalog.
type Manifest struct {
	Runtime Runtime `dynamodbav:"runtime"`
	Inputs  []Port  `dynamodbav:"inputs,omitempty"`
}

// Port is a stored input; Reference is non-nil when it is filled from the
// reference catalog.
type Port struct {
	Reference *struct{} `dynamodbav:"reference,omitempty"`
}

// HasReferenceInputs: an input is filled from the reference catalog, which
// is delivered to ECS steps (standard, gpu) only.
func (m *Manifest) HasReferenceInputs() bool {
	if m == nil {
		return false
	}
	for _, p := range m.Inputs {
		if p.Reference != nil {
			return true
		}
	}
	return false
}

// ImageFacts is what eligibility reads of an image: its platform, its
// labels and its compressed size.
type ImageFacts struct {
	OS              string
	Architecture    string
	Labels          map[string]string
	CompressedBytes int64
}

// Decide is the eligibility of a runtime and image, checked at now.
func Decide(rt Runtime, img ImageFacts, hasReferenceInputs bool, now time.Time) Eligibility {
	var reasons []string
	if rt.Optimize != nil && !*rt.Optimize {
		reasons = append(reasons, "the app opts out of optimized runtimes (runtime.optimize: false)")
	}
	if gpuOnly(rt.ComputeTypes) {
		reasons = append(reasons, "it runs on GPU only")
	}
	if rt.Interactive || rt.SessionType != "" {
		reasons = append(reasons, "interactive sessions don't run on Lambda")
	}
	if hasReferenceInputs {
		reasons = append(reasons, "it reads reference data, which a Lambda step can't")
	}
	if rt.Memory > MaxMemoryMiB {
		reasons = append(reasons, fmt.Sprintf("it needs %s GB of memory; a Lambda step has at most 10 GB", units(rt.Memory, 1024)))
	}
	if rt.CPU > MaxCPUUnits {
		reasons = append(reasons, fmt.Sprintf("it needs %s vCPU; a Lambda step has at most 6", units(rt.CPU, 1024)))
	}
	if rt.TimeoutSeconds > MaxTimeoutSeconds {
		reasons = append(reasons, fmt.Sprintf("it may run up to %s; a Lambda step runs at most 15 minutes", duration(rt.TimeoutSeconds)))
	}
	if img.CompressedBytes > MaxImageBytes {
		reasons = append(reasons, fmt.Sprintf("the image is %.1f GB; Lambda takes images up to 10 GB", float64(img.CompressedBytes)/1e9))
	}
	if img.Labels[ImageLabel] == "" {
		reasons = append(reasons, "the image has no Lambda step runtime: rebuild the app")
	}
	if (img.OS != "" && img.OS != "linux") || img.Architecture != "amd64" {
		reasons = append(reasons, "the image is not linux/amd64")
	}
	return Eligibility{
		Eligible:   len(reasons) == 0,
		Reasons:    reasons,
		ImageBytes: img.CompressedBytes,
		CheckedAt:  now.UTC().Format(time.RFC3339),
	}
}

// gpuOnly: computeTypes lists gpu and nothing else.
func gpuOnly(computeTypes []string) bool {
	if len(computeTypes) == 0 {
		return false
	}
	for _, t := range computeTypes {
		if !strings.EqualFold(strings.TrimSpace(t), "gpu") {
			return false
		}
	}
	return true
}

// units is n in whole units of size, or to one decimal when it isn't whole:
// 16384 MiB is "16", 6656 CPU units "6.5".
func units(n, size int) string {
	if n%size == 0 {
		return fmt.Sprintf("%d", n/size)
	}
	return fmt.Sprintf("%.1f", float64(n)/float64(size))
}

// duration is a timeout as a person reads it: "2 hours", "20 minutes",
// "900 seconds".
func duration(seconds int) string {
	plural := func(n int, unit string) string {
		if n == 1 {
			return fmt.Sprintf("1 %s", unit)
		}
		return fmt.Sprintf("%d %ss", n, unit)
	}
	switch {
	case seconds%3600 == 0:
		return plural(seconds/3600, "hour")
	case seconds%60 == 0:
		return plural(seconds/60, "minute")
	default:
		return plural(seconds, "second")
	}
}

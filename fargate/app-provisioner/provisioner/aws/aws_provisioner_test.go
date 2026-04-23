package provisioner

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

type fakeS3 struct {
	headErr         error
	createErr       error
	versioningErr   error
	headCalls       int
	createCalls     int
	versioningCalls int
	lastCreateInput *s3.CreateBucketInput
}

func (f *fakeS3) HeadBucket(ctx context.Context, params *s3.HeadBucketInput, optFns ...func(*s3.Options)) (*s3.HeadBucketOutput, error) {
	f.headCalls++
	return &s3.HeadBucketOutput{}, f.headErr
}

func (f *fakeS3) CreateBucket(ctx context.Context, params *s3.CreateBucketInput, optFns ...func(*s3.Options)) (*s3.CreateBucketOutput, error) {
	f.createCalls++
	f.lastCreateInput = params
	return &s3.CreateBucketOutput{}, f.createErr
}

func (f *fakeS3) PutBucketVersioning(ctx context.Context, params *s3.PutBucketVersioningInput, optFns ...func(*s3.Options)) (*s3.PutBucketVersioningOutput, error) {
	f.versioningCalls++
	return &s3.PutBucketVersioningOutput{}, f.versioningErr
}

func TestEnsureBackendBucket_BucketExists(t *testing.T) {
	f := &fakeS3{}
	if err := ensureBackendBucket(context.Background(), f, "tfstate-123", "us-east-1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f.headCalls != 1 {
		t.Errorf("expected 1 HeadBucket call, got %d", f.headCalls)
	}
	if f.createCalls != 0 || f.versioningCalls != 0 {
		t.Errorf("expected no create/configure calls when bucket exists")
	}
}

func TestEnsureBackendBucket_CreatesWhenMissing_USEast1(t *testing.T) {
	f := &fakeS3{headErr: &s3types.NotFound{}}
	if err := ensureBackendBucket(context.Background(), f, "tfstate-123", "us-east-1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f.createCalls != 1 {
		t.Fatalf("expected CreateBucket to be called once, got %d", f.createCalls)
	}
	if f.lastCreateInput.CreateBucketConfiguration != nil {
		t.Errorf("us-east-1 create should not set LocationConstraint")
	}
	if f.versioningCalls != 1 {
		t.Errorf("expected versioning to be called once, got %d", f.versioningCalls)
	}
}

func TestEnsureBackendBucket_CreatesWithLocationConstraint(t *testing.T) {
	f := &fakeS3{headErr: &s3types.NotFound{}}
	if err := ensureBackendBucket(context.Background(), f, "tfstate-123", "us-west-2"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f.lastCreateInput.CreateBucketConfiguration == nil {
		t.Fatal("expected LocationConstraint for non-us-east-1 region")
	}
	if got := string(f.lastCreateInput.CreateBucketConfiguration.LocationConstraint); got != "us-west-2" {
		t.Errorf("expected LocationConstraint us-west-2, got %s", got)
	}
}

func TestEnsureBackendBucket_HandlesNoSuchBucket(t *testing.T) {
	f := &fakeS3{headErr: &s3types.NoSuchBucket{}}
	if err := ensureBackendBucket(context.Background(), f, "tfstate-123", "us-east-1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f.createCalls != 1 {
		t.Errorf("expected CreateBucket to be called, got %d", f.createCalls)
	}
}

func TestEnsureBackendBucket_PropagatesUnexpectedHeadError(t *testing.T) {
	f := &fakeS3{headErr: errors.New("access denied")}
	err := ensureBackendBucket(context.Background(), f, "tfstate-123", "us-east-1")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if f.createCalls != 0 {
		t.Errorf("expected no CreateBucket call on head error, got %d", f.createCalls)
	}
}

func TestEnsureBackendBucket_AlreadyOwnedByYouIsOK(t *testing.T) {
	f := &fakeS3{
		headErr:   &s3types.NotFound{},
		createErr: &s3types.BucketAlreadyOwnedByYou{},
	}
	if err := ensureBackendBucket(context.Background(), f, "tfstate-123", "us-east-1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f.versioningCalls != 1 {
		t.Errorf("expected versioning to run after already-owned; got %d", f.versioningCalls)
	}
}

func TestEnsureBackendBucket_PropagatesCreateError(t *testing.T) {
	f := &fakeS3{
		headErr:   &s3types.NotFound{},
		createErr: errors.New("boom"),
	}
	if err := ensureBackendBucket(context.Background(), f, "tfstate-123", "us-east-1"); err == nil {
		t.Fatal("expected error, got nil")
	}
	if f.versioningCalls != 0 {
		t.Errorf("expected no versioning call on create error")
	}
}

func TestEnsureBackendBucket_PropagatesVersioningError(t *testing.T) {
	f := &fakeS3{
		headErr:       &s3types.NotFound{},
		versioningErr: errors.New("boom"),
	}
	if err := ensureBackendBucket(context.Background(), f, "tfstate-123", "us-east-1"); err == nil {
		t.Fatal("expected error, got nil")
	}
}

package backends

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"cloud.google.com/go/storage"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"google.golang.org/api/option"
)

// These tests exercise the SDK-wrapping adapter constructors and confirm the
// returned wrappers conform to the expected client interfaces. They cannot
// reach real cloud infrastructure, so they assert wrapper-level invariants
// (interface satisfaction, constructor non-nil) and exercise the adapter
// methods enough to cover the SDK call-site lines.

func TestNewAWSS3Adapter(t *testing.T) {
	cli := s3.New(s3.Options{Region: "us-east-1"})
	a := NewAWSS3Adapter(cli)
	if a == nil {
		t.Fatal("nil")
	}
	if a.client == nil || a.uploader == nil {
		t.Fatal("client/uploader not set")
	}
	var _ S3Client = a
}

func TestAWSS3Adapter_ErrorPropagation(t *testing.T) {
	// Point the SDK at a server that always returns 4xx so the adapter wrappers
	// run their call-site code paths and return the wrapped error.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	cli := s3.New(s3.Options{
		Region:           "us-east-1",
		BaseEndpoint:     aws.String(srv.URL),
		UsePathStyle:     true,
		RetryMaxAttempts: 1,
		Credentials:      staticCreds{},
	})
	a := NewAWSS3Adapter(cli)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if _, err := a.GetObject(ctx, "bucket", "key"); err == nil {
		t.Fatal("GetObject expected err")
	}
	if err := a.PutObject(ctx, "bucket", "key", strings.NewReader("payload")); err == nil {
		t.Fatal("PutObject expected err")
	}
	if err := a.DeleteObject(ctx, "bucket", "key"); err == nil {
		t.Fatal("DeleteObject expected err")
	}
	if _, err := a.ObjectSize(ctx, "bucket", "key"); err == nil {
		t.Fatal("ObjectSize expected err")
	}
	if exists, err := a.ObjectExists(ctx, "bucket", "key"); err != nil || exists {
		t.Fatalf("ObjectExists: %v %v", exists, err)
	}
	if err := a.CopyObject(ctx, "bucket", "src", "dst"); err == nil {
		t.Fatal("CopyObject expected err")
	}
}

type staticCreds struct{}

func (staticCreds) Retrieve(_ context.Context) (aws.Credentials, error) {
	return aws.Credentials{AccessKeyID: "x", SecretAccessKey: "y"}, nil
}

func TestNewGCSStorageAdapter(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	cli, err := storage.NewClient(ctx,
		option.WithEndpoint(srv.URL),
		option.WithoutAuthentication(),
		option.WithHTTPClient(srv.Client()),
	)
	if err != nil {
		t.Fatalf("storage.NewClient: %v", err)
	}
	defer cli.Close()

	a := NewGCSStorageAdapter(cli)
	if a == nil {
		t.Fatal("nil")
	}
	var _ GCSClient = a

	// Exercise the adapter wrappers — calls go to the fake 404 server, so each
	// surfaces an error but the wrapper code path is covered.
	if w := a.NewWriter(ctx, "bucket", "object"); w == nil {
		t.Fatal("NewWriter nil")
	}
	if _, err := a.NewReader(ctx, "bucket", "object"); err == nil {
		t.Fatal("NewReader expected err")
	}
	if err := a.Delete(ctx, "bucket", "object"); err == nil {
		t.Fatal("Delete expected err")
	}
	if _, err := a.Attrs(ctx, "bucket", "object"); err == nil {
		t.Fatal("Attrs expected err")
	}
	if err := a.Copy(ctx, "bucket", "src", "dst"); err == nil {
		t.Fatal("Copy expected err")
	}
	it := a.List(ctx, "bucket", "prefix")
	if _, err := it.NextName(); err == nil {
		t.Fatal("NextName expected err")
	}
}

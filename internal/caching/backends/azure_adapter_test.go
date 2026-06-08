package backends

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"

	"grog/internal/config"
)

// azureFakeServer is a minimal HTTPS-like endpoint the azblob client can
// connect to. Every response is 4xx so the adapter wrappers exercise their
// call-site code paths and return the wrapped errors.
func azureFakeServer(t *testing.T, status int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if status == http.StatusNotFound {
			w.Header().Set("x-ms-error-code", "BlobNotFound")
		}
		w.WriteHeader(status)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func newAzureAdapterForTest(t *testing.T, srv *httptest.Server) *AzureBlobAdapter {
	t.Helper()
	cli, err := azblob.NewClientWithNoCredential(srv.URL+"/devstoreaccount1", &azblob.ClientOptions{})
	if err != nil {
		t.Fatalf("NewClientWithNoCredential: %v", err)
	}
	return NewAzureBlobAdapter(cli)
}

func TestAzureBlobAdapter_GetBlob_Error(t *testing.T) {
	srv := azureFakeServer(t, http.StatusNotFound)
	a := newAzureAdapterForTest(t, srv)
	if _, err := a.GetBlob(context.Background(), "container", "blob"); err == nil {
		t.Fatal("expected err")
	}
}

func TestAzureBlobAdapter_UploadBlob_Error(t *testing.T) {
	srv := azureFakeServer(t, http.StatusForbidden)
	a := newAzureAdapterForTest(t, srv)
	if err := a.UploadBlob(context.Background(), "container", "blob", strings.NewReader("x")); err == nil {
		t.Fatal("expected err")
	}
}

func TestAzureBlobAdapter_DeleteBlob_Error(t *testing.T) {
	srv := azureFakeServer(t, http.StatusForbidden)
	a := newAzureAdapterForTest(t, srv)
	if err := a.DeleteBlob(context.Background(), "container", "blob"); err == nil {
		t.Fatal("expected err")
	}
}

func TestAzureBlobAdapter_BlobExists_NotFound(t *testing.T) {
	srv := azureFakeServer(t, http.StatusNotFound)
	a := newAzureAdapterForTest(t, srv)
	exists, err := a.BlobExists(context.Background(), "container", "blob")
	if err != nil {
		t.Fatalf("BlobExists: %v", err)
	}
	if exists {
		t.Fatal("expected not exists")
	}
}

func TestAzureBlobAdapter_BlobExists_Forbidden(t *testing.T) {
	srv := azureFakeServer(t, http.StatusForbidden)
	a := newAzureAdapterForTest(t, srv)
	if _, err := a.BlobExists(context.Background(), "container", "blob"); err == nil {
		t.Fatal("expected err for non-404")
	}
}

func TestAzureBlobAdapter_BlobSize_Error(t *testing.T) {
	srv := azureFakeServer(t, http.StatusForbidden)
	a := newAzureAdapterForTest(t, srv)
	if _, err := a.BlobSize(context.Background(), "container", "blob"); err == nil {
		t.Fatal("expected err")
	}
}

func TestAzureBlobAdapter_CopyBlob_Error(t *testing.T) {
	srv := azureFakeServer(t, http.StatusForbidden)
	a := newAzureAdapterForTest(t, srv)
	if err := a.CopyBlob(context.Background(), "container", "src", "dst"); err == nil {
		t.Fatal("expected err")
	}
}

func TestNewAzureCache_ConnectionString_Bad(t *testing.T) {
	_, err := NewAzureCache(context.Background(), config.AzureCacheConfig{
		Container:        "c",
		ConnectionString: "not-a-real-connection-string",
	})
	if err == nil {
		t.Fatal("expected err for bad connection string")
	}
}

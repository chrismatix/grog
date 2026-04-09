package backends

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"

	"grog/internal/config"
	"grog/internal/console"
)

// AzureBlobClient defines the interface for Azure Blob Storage operations.
// This interface allows for easy mocking in tests.
type AzureBlobClient interface {
	// GetBlob retrieves a blob from Azure Blob Storage.
	GetBlob(ctx context.Context, container, blob string) (io.ReadCloser, error)

	// UploadBlob uploads a blob to Azure Blob Storage.
	UploadBlob(ctx context.Context, container, blob string, body io.Reader) error

	// DeleteBlob removes a blob from Azure Blob Storage.
	DeleteBlob(ctx context.Context, container, blob string) error

	// BlobExists checks if a blob exists in Azure Blob Storage.
	BlobExists(ctx context.Context, container, blob string) (bool, error)

	// BlobSize returns the size in bytes of a blob without downloading it.
	BlobSize(ctx context.Context, container, blob string) (int64, error)
}

// AzureBlobAdapter adapts the Azure azblob client to the AzureBlobClient interface.
type AzureBlobAdapter struct {
	client *azblob.Client
}

// NewAzureBlobAdapter creates a new adapter for the Azure Blob Storage client.
func NewAzureBlobAdapter(client *azblob.Client) *AzureBlobAdapter {
	return &AzureBlobAdapter{
		client: client,
	}
}

// GetBlob retrieves a blob from Azure Blob Storage.
func (a *AzureBlobAdapter) GetBlob(ctx context.Context, container, blob string) (io.ReadCloser, error) {
	response, err := a.client.DownloadStream(ctx, container, blob, nil)
	if err != nil {
		return nil, err
	}

	return response.Body, nil
}

// UploadBlob uploads a blob to Azure Blob Storage.
func (a *AzureBlobAdapter) UploadBlob(ctx context.Context, container, blob string, body io.Reader) error {
	_, err := a.client.UploadStream(ctx, container, blob, body, nil)
	return err
}

// DeleteBlob removes a blob from Azure Blob Storage.
func (a *AzureBlobAdapter) DeleteBlob(ctx context.Context, container, blob string) error {
	_, err := a.client.DeleteBlob(ctx, container, blob, nil)
	return err
}

// BlobExists checks if a blob exists in Azure Blob Storage using a lightweight properties request.
func (a *AzureBlobAdapter) BlobExists(ctx context.Context, container, blobName string) (bool, error) {
	blobClient := a.client.ServiceClient().NewContainerClient(container).NewBlobClient(blobName)
	_, err := blobClient.GetProperties(ctx, nil)
	if err != nil {
		// The Azure SDK returns a ResponseError for not-found blobs.
		// Check for the BlobNotFound error code in the response.
		if isBlobNotFoundError(err) {
			return false, nil
		}
		return false, err
	}

	return true, nil
}

// BlobSize returns the size of a blob via a properties request (no body
// download).
func (a *AzureBlobAdapter) BlobSize(ctx context.Context, container, blobName string) (int64, error) {
	blobClient := a.client.ServiceClient().NewContainerClient(container).NewBlobClient(blobName)
	props, err := blobClient.GetProperties(ctx, nil)
	if err != nil {
		return 0, err
	}
	if props.ContentLength == nil {
		return 0, fmt.Errorf("azure get properties: missing content length for %s/%s", container, blobName)
	}
	return *props.ContentLength, nil
}

// isBlobNotFoundError checks whether an error from the Azure SDK indicates that a blob was not found.
func isBlobNotFoundError(err error) bool {
	// The Azure SDK wraps HTTP 404 responses as *azcore.ResponseError.
	// We check the error string for the BlobNotFound code rather than importing
	// azcore directly, keeping the dependency surface minimal.
	return strings.Contains(err.Error(), "BlobNotFound") ||
		strings.Contains(err.Error(), "RESPONSE 404")
}

// AzureCache implements the CacheBackend interface using Azure Blob Storage.
type AzureCache struct {
	containerName   string
	prefix          string
	workspacePrefix string
	client          AzureBlobClient
	logger          *console.Logger
}

// TypeName returns the name of the cache backend.
func (a *AzureCache) TypeName() string {
	return "azure"
}

// NewAzureCache creates a new Azure Blob Storage cache.
func NewAzureCache(
	ctx context.Context,
	cacheConfig config.AzureCacheConfig,
) (*AzureCache, error) {
	if cacheConfig.Container == "" {
		return nil, fmt.Errorf("azure container name is not set")
	}

	var client *azblob.Client

	if cacheConfig.ConnectionString != "" {
		// Authenticate using a connection string (account key based).
		var err error
		client, err = azblob.NewClientFromConnectionString(cacheConfig.ConnectionString, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create Azure client from connection string: %w", err)
		}
	} else if cacheConfig.AccountURL != "" {
		// Authenticate using DefaultAzureCredential (Azure AD / managed identity / CLI).
		credential, err := azidentity.NewDefaultAzureCredential(nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create Azure credential: %w", err)
		}
		client, err = azblob.NewClient(cacheConfig.AccountURL, credential, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create Azure Blob Storage client: %w", err)
		}
	} else {
		return nil, fmt.Errorf("azure cache requires either account_url or connection_string to be set")
	}

	adapter := NewAzureBlobAdapter(client)

	return NewAzureCacheWithClient(ctx, cacheConfig, adapter)
}

// NewAzureCacheWithClient creates a new Azure Blob Storage cache with a provided client.
// This is useful for testing with a mock client.
func NewAzureCacheWithClient(
	ctx context.Context,
	cacheConfig config.AzureCacheConfig,
	client AzureBlobClient,
) (*AzureCache, error) {
	if cacheConfig.Container == "" {
		return nil, fmt.Errorf("azure container name is not set")
	}

	prefix := cacheConfig.Prefix
	if prefix != "" {
		prefix = strings.Trim(prefix, "/")
	}

	workspaceDir := config.Global.WorkspaceRoot
	var workspacePrefix string

	if cacheConfig.SharedCache {
		// If shared cache is enabled treat prefix as the workspace root.
		workspacePrefix = ""
	} else {
		// If shared cache is disabled, use the full path hash.
		workspacePrefix = strings.Trim(config.GetWorkspaceCachePrefix(workspaceDir), "/")
	}

	logger := console.GetLogger(ctx)
	logger.Tracef("Instantiated Azure cache at container %s with prefix %s and workspace dir %s",
		cacheConfig.Container,
		prefix,
		workspacePrefix)
	return &AzureCache{
		logger:          logger,
		client:          client,
		containerName:   cacheConfig.Container,
		prefix:          prefix,
		workspacePrefix: workspacePrefix,
	}, nil
}

func (a *AzureCache) fullPrefix() string {
	if a.prefix == "" {
		return a.workspacePrefix
	}
	if a.workspacePrefix == "" {
		return a.prefix
	}
	return a.prefix + "/" + a.workspacePrefix
}

// buildPath constructs the full Azure blob path for a cached item.
func (a *AzureCache) buildPath(path, key string) string {
	parts := []string{a.fullPrefix(), strings.Trim(path, "/"), strings.Trim(key, "/")}
	return strings.Join(parts, "/")
}

// Get retrieves a cached file from Azure Blob Storage.
func (a *AzureCache) Get(ctx context.Context, path, key string) (io.ReadCloser, error) {
	logger := console.GetLogger(ctx)
	blobPath := a.buildPath(path, key)
	logger.Tracef("Getting file from Azure for path: %s", blobPath)

	return a.client.GetBlob(ctx, a.containerName, blobPath)
}

// Set stores a file in Azure Blob Storage.
func (a *AzureCache) Set(ctx context.Context, path, key string, content io.Reader) error {
	logger := console.GetLogger(ctx)
	blobPath := a.buildPath(path, key)
	logger.Tracef("Setting file in Azure for path: %s", blobPath)

	return a.client.UploadBlob(ctx, a.containerName, blobPath, content)
}

// Delete removes a cached file from Azure Blob Storage.
func (a *AzureCache) Delete(ctx context.Context, path string, key string) error {
	logger := console.GetLogger(ctx)
	blobPath := a.buildPath(path, key)
	logger.Tracef("Deleting file from Azure for path: %s", blobPath)

	return a.client.DeleteBlob(ctx, a.containerName, blobPath)
}

// Exists checks if a file exists in Azure Blob Storage.
func (a *AzureCache) Exists(ctx context.Context, path string, key string) (bool, error) {
	logger := console.GetLogger(ctx)
	blobPath := a.buildPath(path, key)
	logger.Tracef("Checking existence of file in Azure for path: %s", blobPath)

	return a.client.BlobExists(ctx, a.containerName, blobPath)
}

// Size returns the byte size of a blob in Azure Blob Storage via a properties
// request — no body is downloaded.
func (a *AzureCache) Size(ctx context.Context, path, key string) (int64, error) {
	logger := console.GetLogger(ctx)
	blobPath := a.buildPath(path, key)
	logger.Tracef("Sizing blob in Azure for path: %s", blobPath)

	return a.client.BlobSize(ctx, a.containerName, blobPath)
}

// ListKeys uses Azure Blob Storage list blobs to list keys under the given path.
func (a *AzureCache) ListKeys(ctx context.Context, path string, suffix string) ([]string, error) {
	adapter, ok := a.client.(*AzureBlobAdapter)
	if !ok {
		return nil, nil
	}

	fullPath := a.buildPath(path, "")
	if !strings.HasSuffix(fullPath, "/") {
		fullPath += "/"
	}

	var keys []string
	pager := adapter.client.NewListBlobsFlatPager(a.containerName, &azblob.ListBlobsFlatOptions{
		Prefix: &fullPath,
	})
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, blob := range page.Segment.BlobItems {
			name := *blob.Name
			key := strings.TrimPrefix(name, fullPath)
			if suffix == "" || strings.HasSuffix(key, suffix) {
				keys = append(keys, key)
			}
		}
	}
	return keys, nil
}

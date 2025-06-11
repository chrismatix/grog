package backends

import (
	"context"
	"fmt"
	"go.uber.org/zap"
	grogconfig "grog/internal/config"
	"grog/internal/console"
	"io"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
)

// azureBlobClientInterface defines the interface for Azure Blob Storage client operations
type azureBlobClientInterface interface {
	DownloadStream(ctx context.Context, containerName string, blobName string, options *azblob.DownloadStreamOptions) (azblob.DownloadStreamResponse, error)
	UploadStream(ctx context.Context, containerName string, blobName string, body io.Reader, options *azblob.UploadStreamOptions) (azblob.UploadStreamResponse, error)
	DeleteBlob(ctx context.Context, containerName string, blobName string, options *azblob.DeleteBlobOptions) (azblob.DeleteBlobResponse, error)
}

// Ensure that the real Azure Blob Storage client implements our interface
var _ azureBlobClientInterface = (*azblob.Client)(nil)

// AzureBlobCache implements the CacheBackend interface using Azure Blob Storage.
type AzureBlobCache struct {
	containerName   string
	prefix          string
	workspacePrefix string
	client          azureBlobClientInterface
	logger          *zap.SugaredLogger
}

func (a *AzureBlobCache) TypeName() string {
	return "azure"
}

// NewAzureBlobCache creates a new Azure Blob Storage cache.
func NewAzureBlobCache(
	ctx context.Context,
	cacheConfig grogconfig.AzureBlobCacheConfig,
) (*AzureBlobCache, error) {
	if cacheConfig.ContainerName == "" {
		return nil, fmt.Errorf("Azure Blob Storage container name is not set")
	}

	// Create Azure Blob Storage client
	client, err := azblob.NewClientFromConnectionString(cacheConfig.ConnectionString, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create Azure Blob Storage client: %w", err)
	}

	prefix := cacheConfig.Prefix
	if prefix == "" {
		prefix = ""
	} else {
		prefix = strings.Trim(prefix, "/")
	}

	workspaceDir := grogconfig.Global.WorkspaceRoot
	workspacePrefix := strings.Trim(grogconfig.GetWorkspaceCachePrefix(workspaceDir), "/")

	logger := console.GetLogger(ctx)
	logger.Debugf("Instantiated Azure Blob Storage cache at container %s with prefix %s and workspace dir %s",
		cacheConfig.ContainerName,
		prefix,
		workspacePrefix)
	return &AzureBlobCache{
		logger:          logger,
		client:          client,
		containerName:   cacheConfig.ContainerName,
		prefix:          prefix,
		workspacePrefix: workspacePrefix,
	}, nil
}

func (a *AzureBlobCache) fullPrefix() string {
	if a.prefix == "" {
		return a.workspacePrefix
	}
	return a.prefix + "/" + a.workspacePrefix
}

// buildPath constructs the full Azure Blob Storage path for a cached item.
func (a *AzureBlobCache) buildPath(path, key string) string {
	parts := []string{a.fullPrefix(), strings.Trim(path, "/"), strings.Trim(key, "/")}
	return strings.Join(parts, "/")
}

// Get retrieves a cached file from Azure Blob Storage.
func (a *AzureBlobCache) Get(ctx context.Context, path, key string) (io.ReadCloser, error) {
	logger := console.GetLogger(ctx)
	blobPath := a.buildPath(path, key)
	logger.Debugf("Getting file from Azure Blob Storage for path: %s", blobPath)

	// Download the blob
	downloadResponse, err := a.client.DownloadStream(ctx, a.containerName, blobPath, nil)
	if err != nil {
		logger.Debugf("Failed to get file from Azure Blob Storage for path: %s, key: %s: %v", path, key, err)
		return nil, fmt.Errorf("failed to download blob: %w", err)
	}

	return downloadResponse.Body, nil
}

// Set stores a file in Azure Blob Storage.
func (a *AzureBlobCache) Set(ctx context.Context, path, key string, content io.Reader) error {
	logger := console.GetLogger(ctx)
	blobPath := a.buildPath(path, key)
	logger.Debugf("Setting file in Azure Blob Storage for path: %s", blobPath)

	// Upload the blob
	_, err := a.client.UploadStream(ctx, a.containerName, blobPath, content, nil)
	if err != nil {
		return fmt.Errorf("failed to upload blob: %w", err)
	}

	return nil
}

// Delete removes a cached file from Azure Blob Storage.
func (a *AzureBlobCache) Delete(ctx context.Context, path string, key string) error {
	logger := console.GetLogger(ctx)
	blobPath := a.buildPath(path, key)
	logger.Debugf("Deleting file from Azure Blob Storage for path: %s", blobPath)

	// Delete the blob
	_, err := a.client.DeleteBlob(ctx, a.containerName, blobPath, nil)
	if err != nil {
		return fmt.Errorf("failed to delete blob: %w", err)
	}

	return nil
}

// Exists checks if a file exists in Azure Blob Storage.
func (a *AzureBlobCache) Exists(ctx context.Context, path string, key string) (bool, error) {
	logger := console.GetLogger(ctx)
	blobPath := a.buildPath(path, key)
	logger.Debugf("Checking existence of file in Azure Blob Storage for path: %s", blobPath)

	// Try to download the blob properties to check if it exists
	_, err := a.client.DownloadStream(ctx, a.containerName, blobPath, nil)
	if err != nil {
		// If the blob doesn't exist, return false
		logger.Debugf("File does not exist: %s", blobPath)
		return false, nil
	}

	return true, nil
}

// Clear is not supported for remote caches
func (a *AzureBlobCache) Clear(ctx context.Context, expunge bool) error {
	return nil
}

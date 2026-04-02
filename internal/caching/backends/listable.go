package backends

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"cloud.google.com/go/storage"
	"google.golang.org/api/iterator"
)

// ListableBackend extends CacheBackend with key listing capability.
type ListableBackend interface {
	CacheBackend

	// ListKeys returns all keys under the given path that match the given suffix.
	// Keys are returned as relative paths within the path prefix (e.g. "2026-03-30/trace-id.parquet").
	ListKeys(ctx context.Context, path string, suffix string) ([]string, error)
}

// FileSystemCache.ListKeys walks the directory tree.
func (fsc *FileSystemCache) ListKeys(ctx context.Context, path string, suffix string) ([]string, error) {
	dir := fsc.getDir(path)

	var keys []string
	err := filepath.Walk(dir, func(filePath string, info os.FileInfo, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(dir, filePath)
		if relErr != nil {
			return relErr
		}
		if suffix == "" || strings.HasSuffix(rel, suffix) {
			keys = append(keys, rel)
		}
		return nil
	})

	if err != nil && os.IsNotExist(err) {
		return nil, nil
	}
	return keys, err
}

// S3Cache.ListKeys uses S3 ListObjectsV2 with prefix filtering.
func (sc *S3Cache) ListKeys(ctx context.Context, path string, suffix string) ([]string, error) {
	adapter, ok := sc.client.(*AWSS3Adapter)
	if !ok {
		return nil, nil // mock client in tests — skip listing
	}

	fullPath := sc.buildPath(path, "")
	if !strings.HasSuffix(fullPath, "/") {
		fullPath += "/"
	}

	var keys []string
	paginator := s3.NewListObjectsV2Paginator(adapter.client, &s3.ListObjectsV2Input{
		Bucket: aws.String(sc.bucketName),
		Prefix: aws.String(fullPath),
	})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, obj := range page.Contents {
			key := strings.TrimPrefix(*obj.Key, fullPath)
			if suffix == "" || strings.HasSuffix(key, suffix) {
				keys = append(keys, key)
			}
		}
	}
	return keys, nil
}

// GCSCache.ListKeys uses GCS Objects.List with prefix filtering.
func (gcs *GCSCache) ListKeys(ctx context.Context, path string, suffix string) ([]string, error) {
	fullPath := gcs.buildPath(path, "")
	if !strings.HasSuffix(fullPath, "/") {
		fullPath += "/"
	}

	it := gcs.client.Bucket(gcs.bucketName).Objects(ctx, &storage.Query{
		Prefix: fullPath,
	})

	var keys []string
	for {
		attrs, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, err
		}
		key := strings.TrimPrefix(attrs.Name, fullPath)
		if suffix == "" || strings.HasSuffix(key, suffix) {
			keys = append(keys, key)
		}
	}
	return keys, nil
}

// AzureCache.ListKeys uses Azure Blob Storage list blobs with prefix filtering.
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

// RemoteWrapper.ListKeys delegates to the remote backend for a complete
// picture of all traces (including those from other machines).
func (rw *RemoteWrapper) ListKeys(ctx context.Context, path string, suffix string) ([]string, error) {
	if listable, ok := rw.remote.(ListableBackend); ok {
		return listable.ListKeys(ctx, path, suffix)
	}
	return rw.fs.ListKeys(ctx, path, suffix)
}

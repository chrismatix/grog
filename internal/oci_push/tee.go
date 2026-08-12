package oci_push

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"sync"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/stream"
	"github.com/google/go-containerregistry/pkg/v1/types"

	"grog/internal/ociproxy"
)

// Tee opportunistically streams blobs to oci_push destination repositories
// while the daemon is still uploading them to the cache. Any failure just
// leaves blobs for the subsequent push plan to re-upload from the CAS.
type Tee struct {
	ctx    context.Context
	cancel context.CancelFunc
	repos  []name.Repository

	uploads sync.WaitGroup

	mu          sync.Mutex
	uploadCount int
	failedRepos map[string]error
}

var _ ociproxy.BlobTee = (*Tee)(nil)

// NewTee parses and deduplicates the destination references into repositories.
// isInsecure reports whether a destination is a declared plain-HTTP registry.
func NewTee(ctx context.Context, destinations []string, isInsecure func(string) bool) (*Tee, error) {
	seen := make(map[string]bool)
	var repos []name.Repository
	for _, destination := range destinations {
		ref, err := parseRef(destination, isInsecure(destination))
		if err != nil {
			return nil, fmt.Errorf("parse push destination %q: %w", destination, err)
		}
		repo := ref.Context()
		if seen[repo.String()] {
			continue
		}
		seen[repo.String()] = true
		repos = append(repos, repo)
	}
	teeCtx, cancel := context.WithCancel(ctx)
	return &Tee{ctx: teeCtx, cancel: cancel, repos: repos, failedRepos: make(map[string]error)}, nil
}

// OpenBlob starts one streaming upload per destination repository and returns
// a sink that fans the blob's bytes out to all of them.
func (t *Tee) OpenBlob() ociproxy.BlobSink {
	sink := &blobSink{}
	for _, repo := range t.repos {
		if t.hasFailed(repo) {
			continue
		}
		pipeReader, pipeWriter := io.Pipe()
		blob := &streamedBlob{reader: pipeReader}
		t.uploads.Add(1)
		go func(repo name.Repository) {
			defer t.uploads.Done()
			err := remote.WriteLayer(repo, blob,
				remote.WithContext(t.ctx),
				remote.WithAuthFromKeychain(authn.DefaultKeychain),
			)
			if err != nil {
				t.recordFailure(repo, err)
				_ = pipeReader.CloseWithError(err)
				return
			}
			t.recordUpload()
		}(repo)
		sink.uploads = append(sink.uploads, &blobUpload{writer: pipeWriter, blob: blob})
	}
	if len(sink.uploads) == 0 {
		return nil
	}
	return sink
}

// Abort cancels all in-flight uploads, e.g. when the daemon push that feeds
// the tee has failed and abandoned upload sessions would otherwise leave
// destination uploads blocked forever.
func (t *Tee) Abort() {
	t.cancel()
}

// Wait blocks until every started upload has settled and reports the number
// of successful blob uploads across all destination repositories plus the
// first error per failed repository.
func (t *Tee) Wait() (int, map[string]error) {
	t.uploads.Wait()
	t.cancel()
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.uploadCount, maps.Clone(t.failedRepos)
}

func (t *Tee) hasFailed(repo name.Repository) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	_, failed := t.failedRepos[repo.String()]
	return failed
}

func (t *Tee) recordFailure(repo name.Repository, err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, exists := t.failedRepos[repo.String()]; !exists {
		t.failedRepos[repo.String()] = err
	}
}

func (t *Tee) recordUpload() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.uploadCount++
}

type blobUpload struct {
	writer *io.PipeWriter
	blob   *streamedBlob
	dead   bool
}

// blobSink fans one blob's bytes out to every destination upload. Per the
// ociproxy.BlobSink contract it is best-effort: a failed destination is
// dropped and Write always reports success to the proxy.
type blobSink struct {
	uploads []*blobUpload
	size    int64
}

func (s *blobSink) Write(p []byte) (int, error) {
	for _, upload := range s.uploads {
		if upload.dead {
			continue
		}
		if _, err := upload.writer.Write(p); err != nil {
			upload.dead = true
		}
	}
	s.size += int64(len(p))
	return len(p), nil
}

func (s *blobSink) Commit(digest string) {
	hash, err := v1.NewHash(digest)
	if err != nil {
		s.Cancel()
		return
	}
	for _, upload := range s.uploads {
		upload.blob.finalize(hash, s.size)
		_ = upload.writer.Close()
	}
}

func (s *blobSink) Cancel() {
	for _, upload := range s.uploads {
		_ = upload.writer.CloseWithError(errors.New("blob upload cancelled"))
	}
}

// streamedBlob is a v1.Layer over an in-flight blob upload. Digest/Size answer
// stream.ErrNotComputed until finalize, which makes remote.WriteLayer stream
// the bytes first and only then ask for the digest to commit the upload.
type streamedBlob struct {
	reader *io.PipeReader

	mu       sync.Mutex
	consumed bool
	done     bool
	digest   v1.Hash
	size     int64
}

func (b *streamedBlob) finalize(digest v1.Hash, size int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.digest = digest
	b.size = size
	b.done = true
}

func (b *streamedBlob) Compressed() (io.ReadCloser, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.consumed {
		return nil, stream.ErrConsumed
	}
	b.consumed = true
	return b.reader, nil
}

func (b *streamedBlob) Uncompressed() (io.ReadCloser, error) {
	return nil, errors.New("streamed blob: uncompressed access not supported")
}

func (b *streamedBlob) Digest() (v1.Hash, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.done {
		return v1.Hash{}, stream.ErrNotComputed
	}
	return b.digest, nil
}

func (b *streamedBlob) DiffID() (v1.Hash, error) {
	return v1.Hash{}, stream.ErrNotComputed
}

func (b *streamedBlob) Size() (int64, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.done {
		return 0, stream.ErrNotComputed
	}
	return b.size, nil
}

func (b *streamedBlob) MediaType() (types.MediaType, error) {
	return types.DockerLayer, nil
}

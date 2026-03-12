package caching

import (
	"bytes"
	"context"
	"io"

	"grog/internal/caching/backends"
	"grog/internal/proto/gen"

	"google.golang.org/protobuf/proto"
)

type TargetResultCache struct {
	backend backends.CacheBackend
}

func NewTargetResultCache(
	cache backends.CacheBackend,
) *TargetResultCache {
	return &TargetResultCache{backend: cache}
}

func (tc *TargetResultCache) GetBackend() backends.CacheBackend {
	return tc.backend
}

func (tc *TargetResultCache) Write(ctx context.Context, targetResult *gen.TargetResult) error {
	marshalledBytes, err := proto.MarshalOptions{Deterministic: true}.Marshal(targetResult)
	if err != nil {
		return err
	}

	return tc.backend.Set(ctx, "target", targetResult.ChangeHash, bytes.NewReader(marshalledBytes))
}

// WriteLocal writes the target result to local FS only.
// For RemoteWrapper: writes to the local FS. For plain FS: same as Write().
func (tc *TargetResultCache) WriteLocal(ctx context.Context, targetResult *gen.TargetResult) error {
	marshalledBytes, err := proto.MarshalOptions{Deterministic: true}.Marshal(targetResult)
	if err != nil {
		return err
	}

	if rw, ok := tc.backend.(*backends.RemoteWrapper); ok {
		return rw.GetFS().Set(ctx, "target", targetResult.ChangeHash, bytes.NewReader(marshalledBytes))
	}
	return tc.backend.Set(ctx, "target", targetResult.ChangeHash, bytes.NewReader(marshalledBytes))
}

// MakeUploadRemoteFunc returns a closure that uploads the target result to the remote backend.
// Returns nil if the backend is not a RemoteWrapper.
func (tc *TargetResultCache) MakeUploadRemoteFunc(changeHash string) func(context.Context) error {
	rw, ok := tc.backend.(*backends.RemoteWrapper)
	if !ok {
		return nil
	}
	return func(ctx context.Context) error {
		reader, err := rw.GetFS().Get(ctx, "target", changeHash)
		if err != nil {
			return err
		}
		defer reader.Close()
		return rw.GetRemote().Set(ctx, "target", changeHash, reader)
	}
}

func (tc *TargetResultCache) Load(ctx context.Context, changeHash string) (*gen.TargetResult, error) {
	reader, err := tc.backend.Get(ctx, "target", changeHash)
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	targetResult := &gen.TargetResult{}
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	if err := proto.Unmarshal(data, targetResult); err != nil {
		return nil, err
	}
	return targetResult, nil
}

func (tc *TargetResultCache) Has(ctx context.Context, changeHash string) (bool, error) {
	return tc.backend.Exists(ctx, "target", changeHash)
}

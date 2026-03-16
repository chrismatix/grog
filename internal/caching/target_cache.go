package caching

import (
	"bytes"
	"context"
	"grog/internal/caching/backends"
	"grog/internal/proto/gen"
	"io"

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

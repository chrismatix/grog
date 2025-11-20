package caching

import (
	"bytes"
	"context"
	"grog/internal/caching/backends"
	v1 "grog/internal/proto/gen"
	"io"

	"github.com/golang/protobuf/proto"
)

type TargetCacheNew struct {
	backend backends.CacheBackend
}

func NewTargetCacheNew(
	cache backends.CacheBackend,
) *TargetCacheNew {
	return &TargetCacheNew{backend: cache}
}

func (tc *TargetCacheNew) GetBackend() backends.CacheBackend {
	return tc.backend
}

func (tc *TargetCacheNew) Write(ctx context.Context, targetResult *v1.TargetResult) error {
	marshalledBytes, err := proto.Marshal(targetResult)
	if err != nil {
		return err
	}

	return tc.backend.Set(ctx, "target", targetResult.ChangeHash, bytes.NewReader(marshalledBytes))
}

func (tc *TargetCacheNew) Load(ctx context.Context, changeHash string) (*v1.TargetResult, error) {
	reader, err := tc.backend.Get(ctx, "target", changeHash)
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	targetResult := &v1.TargetResult{}
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	if err := proto.Unmarshal(data, targetResult); err != nil {
		return nil, err
	}
	return targetResult, nil
}

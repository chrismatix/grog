package caching

import (
	"bytes"
	"context"
	"grog/internal/caching/backends"
	"grog/internal/label"
)

type TaintCache struct {
	backend backends.CacheBackend
}

func NewTaintCache(cache backends.CacheBackend) *TaintCache {
	return &TaintCache{backend: cache}
}

func (tc *TaintCache) GetBackend() backends.CacheBackend {
	return tc.backend
}

func (tc *TaintCache) Taint(ctx context.Context, targetLabel label.TargetLabel) error {
	return tc.backend.Set(ctx, "taint", targetLabel.String(), bytes.NewReader([]byte{}))
}

func (tc *TaintCache) IsTainted(ctx context.Context, targetLabel label.TargetLabel) (bool, error) {
	return tc.backend.Exists(ctx, "taint", targetLabel.String())
}

func (tc *TaintCache) Clear(ctx context.Context, targetLabel label.TargetLabel) error {
	return tc.backend.Delete(ctx, "taint", targetLabel.String())
}

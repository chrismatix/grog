package provider

import (
	"context"
	"io"
	"strings"

	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// newTflogWriter returns an io.Writer that forwards grog's log lines to
// Terraform's structured log sink (visible with TF_LOG=info). It never writes
// to stdout — that channel is owned by the go-plugin gRPC protocol, and writing
// to it would corrupt the provider connection. The session builds its grog
// logger on top of this writer.
func newTflogWriter(ctx context.Context) io.Writer {
	return &tflogWriter{ctx: ctx}
}

type tflogWriter struct {
	ctx context.Context
}

func (w *tflogWriter) Write(p []byte) (int, error) {
	msg := strings.TrimRight(string(p), "\n")
	if msg != "" {
		tflog.Info(w.ctx, msg)
	}
	return len(p), nil
}

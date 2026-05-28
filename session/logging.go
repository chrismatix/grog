package session

import (
	"io"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"grog/internal/console"
)

// newLogger builds a headless grog logger. When w is non-nil, grog's log lines
// are written to it (the embedder decides where they go — e.g. Terraform's log
// sink); the encoder emits the message only, with no timestamps or stdout
// access. When w is nil, the default stdout logger is used.
//
// Keeping this construction inside the session package means embedders never
// need to import grog's internal console package (which they cannot, being in a
// different module).
func newLogger(w io.Writer) *console.Logger {
	if w == nil {
		return console.InitLogger()
	}

	encoderCfg := zap.NewProductionEncoderConfig()
	encoderCfg.TimeKey = ""
	encoderCfg.LevelKey = ""
	encoderCfg.CallerKey = ""
	encoderCfg.NameKey = ""

	core := zapcore.NewCore(
		zapcore.NewConsoleEncoder(encoderCfg),
		zapcore.AddSync(w),
		zapcore.InfoLevel,
	)
	return console.NewFromSugared(zap.New(core).Sugar(), zapcore.InfoLevel)
}

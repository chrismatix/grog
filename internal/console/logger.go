package console

import (
	"context"
	"github.com/fatih/color"
	"github.com/spf13/viper"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"strings"
)

// InitLogger returns a new logger
func InitLogger() *zap.SugaredLogger {
	logPath := viper.GetString("log_output_path")
	if logPath == "" {
		// default to stdout
		logPath = "stdout"
	}

	logLevel := viper.GetString("log_level")
	if logLevel == "" {
		logLevel = "info"
	}

	cfg := zap.NewProductionConfig()

	cfg.DisableStacktrace = true
	cfg.DisableCaller = true
	// Define a custom encoder config
	encoderConfig := zap.NewDevelopmentEncoderConfig()
	encoderConfig.TimeKey = ""   // Disable time encoding
	encoderConfig.CallerKey = "" // Disable caller encoding
	encoderConfig.EncodeLevel = CustomLevelEncoder
	encoderConfig.ConsoleSeparator = " "

	var level zapcore.Level
	switch strings.ToLower(logLevel) {
	case "trace":
		level = zap.DebugLevel
		// Trace mode is debug + caller
		cfg.DisableStacktrace = false
		cfg.DisableCaller = false
		encoderConfig.CallerKey = "C"
		encoderConfig.EncodeCaller = zapcore.FullCallerEncoder
	case "debug":
		level = zap.DebugLevel
	case "info":
		level = zap.InfoLevel
	case "warn":
		level = zap.WarnLevel
	case "error":
		level = zap.ErrorLevel
	default:
		level = zap.InfoLevel
	}

	// do not use structured logging by default
	cfg.Encoding = "console"

	cfg.OutputPaths = []string{
		logPath,
	}

	cfg.Level = zap.NewAtomicLevelAt(level)

	// Implement the color option
	MustApplyColorSetting()

	cfg.EncoderConfig = encoderConfig

	logger, err := cfg.Build()
	if err != nil {
		panic(err)
	}

	return logger.Sugar()
}

func SetLogger(ctx context.Context, logger *zap.SugaredLogger) context.Context {
	if storedLogger, ok := ctx.Value("logger").(*zap.SugaredLogger); ok {
		if storedLogger == logger {
			return ctx
		}
	}

	return context.WithValue(ctx, "logger", logger)
}

func GetLogger(ctx context.Context) *zap.SugaredLogger {
	if logger, ok := ctx.Value("logger").(*zap.SugaredLogger); ok {
		return logger
	}
	return InitLogger()
}

func MustApplyColorSetting() {
	colorSetting := viper.GetString("color")
	if colorSetting == "yes" {
		color.NoColor = false
	} else if colorSetting == "no" {
		color.NoColor = true
		// No need to explicitly handle "auto" as the color package will
		// automatically detect if it is a TTY or not.
	}
}

// CustomLevelEncoder matches the way bazel outputs its log levels
func CustomLevelEncoder(level zapcore.Level, enc zapcore.PrimitiveArrayEncoder) {
	enc.AppendString(getMessagePrefix(level))
}

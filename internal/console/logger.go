package console

import (
	"context"
	"fmt"
	"github.com/fatih/color"
	"github.com/spf13/viper"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"strings"
)

// InitLogger returns a ne logger
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

	// Define a custom encoder config
	encoderConfig := zap.NewDevelopmentEncoderConfig()
	encoderConfig.TimeKey = ""   // Disable time encoding
	encoderConfig.CallerKey = "" // Disable caller encoding
	encoderConfig.EncodeLevel = CustomLevelEncoder
	encoderConfig.ConsoleSeparator = " "

	var level zapcore.Level
	switch strings.ToLower(logLevel) {
	case "debug":
		level = zap.DebugLevel
		// In debug mode, we want to see the caller
		encoderConfig.CallerKey = "C"
		encoderConfig.EncodeCaller = zapcore.FullCallerEncoder
	case "info":
		level = zap.InfoLevel
	case "warn":
		level = zap.WarnLevel
	case "error":
		level = zap.ErrorLevel
	default:
		level = zap.InfoLevel
		fmt.Println("Invalid log Level, defaulting to info")
	}

	cfg := zap.NewProductionConfig()
	// do not use structured logging by default
	cfg.Encoding = "console"
	cfg.DisableStacktrace = true
	if level == zap.DebugLevel {
		cfg.DisableStacktrace = false
	}

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
	} else if colorSetting != "auto" {
		panic("invalid color setting: " + colorSetting + ", must be one of: yes, no, auto")
	}
}

// NewTestLogger logger for test usage
func NewTestLogger() *zap.SugaredLogger {
	cfg := zap.NewProductionConfig()
	cfg.Encoding = "console"

	debug := viper.GetBool("debug")
	var logLevel = zap.InfoLevel
	if debug {
		logLevel = zap.DebugLevel
	}

	// always log to stdout
	cfg.OutputPaths = []string{
		"stdout",
	}

	cfg.Level = zap.NewAtomicLevelAt(logLevel)

	// Define a custom encoder config
	encoderConfig := zap.NewDevelopmentEncoderConfig()
	encoderConfig.EncodeLevel = CustomLevelEncoder

	cfg.EncoderConfig = encoderConfig

	logger, err := cfg.Build()
	if err != nil {
		panic(err)
	}

	return logger.Sugar()
}

// CustomLevelEncoder matches the way bazel outputs its log levels
func CustomLevelEncoder(level zapcore.Level, enc zapcore.PrimitiveArrayEncoder) {
	enc.AppendString(getMessagePrefix(level))
}

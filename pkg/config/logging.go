package config

import (
	"fmt"
	"github.com/spf13/viper"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"strings"
)

// GetLogger returns a logger
func GetLogger() *zap.SugaredLogger {
	// startup check: initialize logger
	logPath := viper.GetString("log_output_path")
	if logPath == "" {
		// default to stdout
		logPath = "stdout"
	}

	logLevel := viper.GetString("log_level")
	if logLevel == "" {
		logLevel = "info"
	}

	var level zapcore.Level
	switch strings.ToLower(logLevel) {
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
		fmt.Println("Invalid log level, defaulting to info")
	}

	cfg := zap.NewProductionConfig()
	// do not use structured logging by default
	cfg.Encoding = "console"

	// always log to stdout
	cfg.OutputPaths = []string{
		logPath,
		"stdout",
	}

	cfg.Level = zap.NewAtomicLevelAt(level)

	// Define a custom encoder config
	encoderConfig := zap.NewDevelopmentEncoderConfig()
	encoderConfig.TimeKey = ""   // Disable time encoding
	encoderConfig.CallerKey = "" // Disable caller encoding
	encoderConfig.EncodeLevel = CustomLevelEncoder

	cfg.EncoderConfig = encoderConfig

	logger, err := cfg.Build()
	if err != nil {
		panic(err)
	}

	return logger.Sugar()
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
	var levelText string
	switch level {
	case zapcore.DebugLevel:
		levelText = "\x1b[36mDEBUG\x1b[0m" // Cyan
	case zapcore.InfoLevel:
		levelText = "\x1b[32mINFO\x1b[0m" // Green
	case zapcore.WarnLevel:
		levelText = "\x1b[33mWARN\x1b[0m" // Yellow
	case zapcore.ErrorLevel:
		levelText = "\x1b[31mERROR\x1b[0m" // Red
	case zapcore.FatalLevel:
		levelText = "\x1b[31mFATAL\x1b[0m" // Red
	default:
		levelText = "UNKNOWN"
	}
	enc.AppendString(levelText + ":")
}

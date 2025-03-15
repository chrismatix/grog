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

	// always log to stdout
	cfg.OutputPaths = []string{
		logPath,
		"stdout",
	}

	cfg.Level = zap.NewAtomicLevelAt(level)
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
	logger, err := cfg.Build()
	if err != nil {
		panic(err)
	}

	return logger.Sugar()
}

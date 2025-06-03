package console

import (
	"context"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/fatih/color"
	"github.com/spf13/viper"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"grog/internal/config"
	"strings"
)

// InitLogger returns a new logger that writes to stdout.
func InitLogger() *zap.SugaredLogger {
	return InitLoggerWithTea(nil)
}

// InitLoggerWithTea returns a new logger that writes to the given Program.
// Leave the Program empty to write to stdout
func InitLoggerWithTea(program *tea.Program) *zap.SugaredLogger {
	logPath := config.Global.LogOutputPath
	if logPath == "" {
		// default to stdout
		logPath = "stdout"
	}

	logLevel := config.Global.LogLevel
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

	if program != nil && UseTea() {
		teaCore := zapcore.NewCore(
			zapcore.NewConsoleEncoder(encoderConfig),
			zapcore.AddSync(NewTeaWriter(program)),
			level,
		)

		return zap.New(teaCore).Sugar()
	}

	logger, err := cfg.Build()
	if err != nil {
		panic(err)
	}
	return logger.Sugar()
}

type ctxLoggerKey struct{}

func WithLogger(ctx context.Context, logger *zap.SugaredLogger) context.Context {
	if storedLogger, ok := ctx.Value(ctxLoggerKey{}).(*zap.SugaredLogger); ok {
		if storedLogger == logger {
			return ctx
		}
	}

	return context.WithValue(ctx, ctxLoggerKey{}, logger)
}

type programKey struct{}

// WithTeaLogger returns a new context with a tea logger and the tea Program
func WithTeaLogger(ctx context.Context, program *tea.Program) context.Context {
	loggerCtx := context.WithValue(ctx, ctxLoggerKey{}, InitLoggerWithTea(program))
	return context.WithValue(loggerCtx, programKey{}, program)
}

func GetLogger(ctx context.Context) *zap.SugaredLogger {
	if logger, ok := ctx.Value(ctxLoggerKey{}).(*zap.SugaredLogger); ok {
		return logger
	}
	logger := InitLogger()
	logger.Debugf("no logger found in context, using default logger. This is probably a bug.")
	return logger
}

func GetTeaProgram(ctx context.Context) *tea.Program {
	if program, ok := ctx.Value(programKey{}).(*tea.Program); ok {
		return program
	}
	return nil
}

// WarnOnError is a helper for warning when some defer cleanup
// function returns an error.
func WarnOnError(ctx context.Context, fn func() error) {
	logger := GetLogger(ctx)
	if err := fn(); err != nil {
		logger.Warnf("deferred error: %v", err)
	}
}

func MustApplyColorSetting() {
	colorSetting := viper.GetString("color")
	if colorSetting == "yes" || colorSetting == "1" {
		color.NoColor = false
	} else if colorSetting == "no" || colorSetting == "0" {
		color.NoColor = true
		// No need to explicitly handle "auto" as the color package will
		// automatically detect if it is a TTY or not.
	}
}

// CustomLevelEncoder matches the way bazel outputs its log levels
func CustomLevelEncoder(level zapcore.Level, enc zapcore.PrimitiveArrayEncoder) {
	enc.AppendString(getMessagePrefix(level))
}

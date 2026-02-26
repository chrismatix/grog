package console

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/fatih/color"
	"github.com/spf13/viper"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"grog/internal/config"
)

// TraceLevel represents the most verbose logging level supported by grog.
const TraceLevel zapcore.Level = zapcore.DebugLevel - 1

// Logger wraps zap's SugaredLogger to add a trace level helper and preserve
// the sugared logging ergonomics throughout the codebase.
type Logger struct {
	*zap.SugaredLogger
	traceEnabled bool
}

func newLogger(logger *zap.SugaredLogger, level zapcore.Level) *Logger {
	return &Logger{SugaredLogger: logger, traceEnabled: level <= TraceLevel}
}

// NewFromSugared wraps an existing zap.SugaredLogger into a console.Logger with the given level.
// Use this in tests to adapt observed/test loggers to the console logger type expected by production code.
func NewFromSugared(logger *zap.SugaredLogger, level zapcore.Level) *Logger {
	return newLogger(logger, level)
}

// Tracef logs at the trace level when enabled.
func (l *Logger) Tracef(template string, args ...interface{}) {
	if l == nil || !l.traceEnabled {
		return
	}

	if checkedEntry := l.Desugar().WithOptions(zap.AddCallerSkip(1)).Check(TraceLevel, fmt.Sprintf(template, args...)); checkedEntry != nil {
		checkedEntry.Write()
	}
}

// DebugEnabled reports whether debug level logs are currently enabled.
func (l *Logger) DebugEnabled() bool {
	if l == nil {
		return false
	}
	return l.Desugar().Core().Enabled(zap.DebugLevel)
}

func (l *Logger) With(args ...interface{}) *Logger {
	return &Logger{
		SugaredLogger: l.SugaredLogger.With(args...),
		traceEnabled:  l.traceEnabled,
	}
}

func (l *Logger) Named(name string) *Logger {
	return &Logger{
		SugaredLogger: l.SugaredLogger.Named(name),
		traceEnabled:  l.traceEnabled,
	}
}

func (l *Logger) WithOptions(opts ...zap.Option) *Logger {
	return &Logger{
		SugaredLogger: l.SugaredLogger.WithOptions(opts...),
		traceEnabled:  l.traceEnabled,
	}
}

// InitLogger returns a new logger that writes to stdout.
func InitLogger() *Logger {
	return InitLoggerWithTea(nil)
}

// InitLoggerWithTea returns a new logger that writes to the given Program.
// Leave the Program empty to write to stdout
func InitLoggerWithTea(program *tea.Program) *Logger {
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
		level = TraceLevel
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

		return newLogger(zap.New(teaCore).Sugar(), level)
	}

	logger, err := cfg.Build()
	if err != nil {
		panic(err)
	}
	return newLogger(logger.Sugar(), level)
}

type ctxLoggerKey struct{}

func WithLogger(ctx context.Context, logger *Logger) context.Context {
	if storedLogger, ok := ctx.Value(ctxLoggerKey{}).(*Logger); ok {
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

func GetLogger(ctx context.Context) *Logger {
	if logger, ok := ctx.Value(ctxLoggerKey{}).(*Logger); ok {
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

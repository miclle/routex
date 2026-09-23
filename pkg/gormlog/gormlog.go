// Package gormlog provides a GORM logger adapter that bridges GORM SQL logs
// to the fox logger, enabling structured logging with request-level trace IDs.
package gormlog

import (
	"context"
	"errors"
	"time"

	foxlogger "github.com/fox-gonic/fox/logger"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// DefaultSlowThreshold is the default threshold for slow SQL queries.
var DefaultSlowThreshold = 200 * time.Millisecond

// Logger implements gorm's logger.Interface, bridging GORM SQL logs to fox logger.
// It extracts the trace ID from context to ensure SQL logs carry the same request ID
// as the rest of the request processing chain.
type Logger struct {
	foxlogger.Logger
	slowThreshold time.Duration
}

// New creates a new Logger with the given slow query threshold.
// If slow is zero or negative, DefaultSlowThreshold is used.
func New(slow time.Duration) *Logger {
	log := foxlogger.NewWithoutCaller("").Caller(6).WithFields(map[string]any{"type": "DATABASE"})
	if slow <= 0 {
		slow = DefaultSlowThreshold
	}
	return &Logger{Logger: log, slowThreshold: slow}
}

// fromContext returns a fox logger enriched with the request trace ID from context.
func (l *Logger) fromContext(ctx context.Context) foxlogger.Logger {
	if requestID, ok := ctx.Value(foxlogger.TraceID).(string); ok {
		return l.WithFields(map[string]any{foxlogger.TraceID: requestID})
	}
	if requestID, ok := ctx.Value(foxlogger.TraceIDKey).(string); ok {
		return l.WithFields(map[string]any{foxlogger.TraceID: requestID})
	}
	return l.Logger
}

// LogMode implements gorm's logger.Interface.
func (l *Logger) LogMode(lvl gormlogger.LogLevel) gormlogger.Interface {
	var level foxlogger.Level
	switch lvl {
	case gormlogger.Error:
		level = foxlogger.ErrorLevel
	case gormlogger.Warn:
		level = foxlogger.WarnLevel
	case gormlogger.Info:
		level = foxlogger.InfoLevel
	case gormlogger.Silent:
		level = foxlogger.Disabled
	default:
		level = foxlogger.TraceLevel
	}
	return &Logger{
		Logger:        l.SetLevel(level),
		slowThreshold: l.slowThreshold,
	}
}

// Info implements gorm's logger.Interface.
func (l *Logger) Info(ctx context.Context, s string, vals ...any) {
	l.fromContext(ctx).Infof(s, vals...)
}

// Warn implements gorm's logger.Interface.
func (l *Logger) Warn(ctx context.Context, s string, vals ...any) {
	l.fromContext(ctx).Warnf(s, vals...)
}

// Error implements gorm's logger.Interface.
func (l *Logger) Error(ctx context.Context, s string, vals ...any) {
	l.fromContext(ctx).Errorf(s, vals...)
}

// Trace implements gorm's logger.Interface.
func (l *Logger) Trace(ctx context.Context, begin time.Time, fc func() (string, int64), err error) {
	var (
		elapsed   = time.Since(begin)
		sql, rows = fc()
		fields    = map[string]any{
			"latency":       elapsed.String(),
			"sql":           truncateSQL(sql, 1024),
			"rows_affected": rows,
		}
		log = l.fromContext(ctx)
	)

	switch {
	case err != nil && !errors.Is(err, gorm.ErrRecordNotFound):
		log.WithFields(fields).Errorf("%v", err)
	case elapsed > l.slowThreshold:
		fields["slow_query"] = true
		log.WithFields(fields).Warnf("Elapsed %s exceeded, Max %s", elapsed.String(), l.slowThreshold.String())
	default:
		log.WithFields(fields).Info()
	}
}

// truncateSQL truncates a SQL string to maxLen characters, appending "..." if truncated.
func truncateSQL(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

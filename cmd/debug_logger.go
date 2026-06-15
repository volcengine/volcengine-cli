package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

const (
	envCLIDebug        = "VOLCENGINE_CLI_DEBUG"
	envCLIDebugLogFile = "VOLCENGINE_CLI_DEBUG_LOG_FILE"

	fixedFlagDebug        = "debug"
	fixedFlagDebugLogFile = "debug-log-file"

	defaultDebugValueLimit = 64 * 1024
	maskedDebugValue       = "***MASKED***"
)

type debugOptions struct {
	Enabled bool
	LogFile string
}

type DebugLogger struct {
	enabled bool
	out     io.Writer
	flush   func() error
	close   func() error
}

func (l *DebugLogger) Enabled() bool {
	return l != nil && l.enabled
}

func (l *DebugLogger) Printf(format string, args ...interface{}) {
	if !l.Enabled() || l.out == nil {
		return
	}
	fmt.Fprintf(l.out, "[debug] "+format+"\n", args...)
}

func (l *DebugLogger) Close() error {
	if l == nil {
		return nil
	}
	if l.flush != nil {
		if err := l.flush(); err != nil {
			return err
		}
	}
	if l.close != nil {
		return l.close()
	}
	return nil
}

func newDebugLogger(opts debugOptions, stderr io.Writer) (*DebugLogger, error) {
	if !opts.Enabled {
		return &DebugLogger{enabled: false}, nil
	}

	if strings.TrimSpace(opts.LogFile) == "" {
		if stderr == nil {
			stderr = os.Stderr
		}
		return &DebugLogger{enabled: true, out: stderr}, nil
	}

	file, err := os.OpenFile(opts.LogFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return nil, err
	}
	if err := file.Chmod(0600); err != nil {
		_ = file.Close()
		return nil, err
	}

	writer := bufio.NewWriter(file)
	return &DebugLogger{
		enabled: true,
		out:     writer,
		flush:   writer.Flush,
		close:   file.Close,
	}, nil
}

func resolveDebugOptions(ctx *Context) (debugOptions, error) {
	var opts debugOptions

	if raw, ok := os.LookupEnv(envCLIDebug); ok {
		enabled, err := parseDebugBool(raw, envCLIDebug)
		if err != nil {
			return opts, err
		}
		opts.Enabled = enabled
	}
	if raw, ok := os.LookupEnv(envCLIDebugLogFile); ok {
		opts.LogFile = strings.TrimSpace(raw)
	}

	if ctx != nil && ctx.fixedFlags != nil {
		if f := ctx.fixedFlags.GetByName(fixedFlagDebug); f != nil {
			enabled, err := parseDebugBool(f.GetValue(), "---"+fixedFlagDebug)
			if err != nil {
				return opts, err
			}
			opts.Enabled = enabled
		}
		if f := ctx.fixedFlags.GetByName(fixedFlagDebugLogFile); f != nil {
			opts.LogFile = strings.TrimSpace(f.GetValue())
		}
	}

	return opts, nil
}

func parseDebugBool(raw string, name string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "true", "1", "t", "yes", "y", "on":
		return true, nil
	case "false", "0", "f", "no", "n", "off":
		return false, nil
	default:
		return false, fmt.Errorf("%s must be a boolean value: true/false, 1/0, yes/no, or on/off", name)
	}
}

func formatDebugValue(value interface{}, limit int) string {
	if limit <= 0 {
		limit = defaultDebugValueLimit
	}

	sanitized := sanitizeDebugValue(value)
	if s, ok := sanitized.(string); ok {
		return truncateDebugString(s, limit)
	}

	data, err := json.MarshalIndent(sanitized, "", "  ")
	if err != nil {
		return truncateDebugString(fmt.Sprintf("%v", sanitized), limit)
	}
	return truncateDebugString(string(data), limit)
}

func truncateDebugString(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return fmt.Sprintf("%s... [truncated, %d bytes omitted]", value[:limit], len(value)-limit)
}

func sanitizeDebugValue(value interface{}) interface{} {
	switch v := value.(type) {
	case map[string]interface{}:
		return sanitizeDebugMap(v)
	case *map[string]interface{}:
		if v == nil {
			return nil
		}
		return sanitizeDebugMap(*v)
	case map[string]string:
		out := make(map[string]interface{}, len(v))
		for key, val := range v {
			if isSensitiveDebugKey(key) {
				out[key] = maskedDebugValue
			} else {
				out[key] = val
			}
		}
		return out
	case http.Header:
		out := make(map[string]interface{}, len(v))
		for key, vals := range v {
			if isSensitiveDebugKey(key) {
				out[key] = maskedDebugValue
			} else {
				out[key] = vals
			}
		}
		return out
	case []interface{}:
		out := make([]interface{}, len(v))
		for i, item := range v {
			out[i] = sanitizeDebugValue(item)
		}
		return out
	default:
		return value
	}
}

func sanitizeDebugMap(input map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(input))
	for key, value := range input {
		if isSensitiveDebugKey(key) {
			out[key] = maskedDebugValue
			continue
		}
		out[key] = sanitizeDebugValue(value)
	}
	return out
}

func isSensitiveDebugKey(key string) bool {
	normalized := strings.ToLower(strings.ReplaceAll(key, "-", "_"))
	compact := strings.ReplaceAll(normalized, "_", "")
	for _, token := range []string{
		"access_key",
		"accesskey",
		"secret",
		"token",
		"authorization",
		"credential",
		"password",
		"cookie",
	} {
		if strings.Contains(normalized, token) || strings.Contains(compact, token) {
			return true
		}
	}
	return false
}

func debugLoggerFromContext(ctx *Context) *DebugLogger {
	if ctx == nil || ctx.debugLogger == nil || !ctx.debugLogger.Enabled() {
		return nil
	}
	return ctx.debugLogger
}

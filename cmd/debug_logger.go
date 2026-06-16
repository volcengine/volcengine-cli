package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"reflect"
	"strings"
	"unicode/utf8"
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
	err     error
}

func (l *DebugLogger) Enabled() bool {
	return l != nil && l.enabled
}

func (l *DebugLogger) Printf(format string, args ...interface{}) {
	if !l.Enabled() || l.out == nil {
		return
	}
	if _, err := fmt.Fprintf(l.out, "[debug] "+format+"\n", args...); err != nil && l.err == nil {
		l.err = err
	}
}

func (l *DebugLogger) Close() error {
	if l == nil {
		return nil
	}
	closeErr := l.err
	if l.flush != nil {
		if err := l.flush(); closeErr == nil && err != nil {
			closeErr = err
		}
	}
	if l.close != nil {
		if err := l.close(); closeErr == nil && err != nil {
			closeErr = err
		}
	}
	return closeErr
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

	file, err := openDebugLogFile(opts.LogFile)
	if err != nil {
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

// openDebugLogFile 在真正写入前后都校验路径，避免 debug 内容被追加到 symlink/hardlink 指向的非预期文件。
func openDebugLogFile(path string) (*os.File, error) {
	if err := rejectUnsafeDebugLogPath(path); err != nil {
		return nil, err
	}

	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return nil, err
	}
	if err := verifyOpenedDebugLogFile(path, file); err != nil {
		_ = file.Close()
		return nil, err
	}
	if err := file.Chmod(0600); err != nil {
		_ = file.Close()
		return nil, err
	}
	return file, nil
}

func rejectUnsafeDebugLogPath(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return validateDebugLogFileInfo(path, info)
}

func verifyOpenedDebugLogFile(path string, file *os.File) error {
	pathInfo, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if err := validateDebugLogFileInfo(path, pathInfo); err != nil {
		return err
	}
	fileInfo, err := file.Stat()
	if err != nil {
		return err
	}
	if !os.SameFile(pathInfo, fileInfo) {
		return fmt.Errorf("debug log file changed while opening: %s", path)
	}
	return nil
}

func validateDebugLogFileInfo(path string, info os.FileInfo) error {
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("debug log file must not be a symbolic link: %s", path)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("debug log file must be a regular file: %s", path)
	}
	if count := hardLinkCount(info); count > 1 {
		return fmt.Errorf("debug log file must not have multiple hard links: %s", path)
	}
	return nil
}

func hardLinkCount(info os.FileInfo) uint64 {
	if info == nil || info.Sys() == nil {
		return 0
	}
	value := reflect.Indirect(reflect.ValueOf(info.Sys()))
	if !value.IsValid() {
		return 0
	}
	field := value.FieldByName("Nlink")
	if !field.IsValid() {
		return 0
	}
	switch field.Kind() {
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return field.Uint()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if field.Int() > 0 {
			return uint64(field.Int())
		}
	}
	return 0
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

	sanitized := sanitizeDebugValue(normalizeDebugValue(value))
	if s, ok := sanitized.(string); ok {
		return truncateDebugString(s, limit)
	}

	data, err := json.MarshalIndent(sanitized, "", "  ")
	if err != nil {
		return truncateDebugString(fmt.Sprintf("%v", sanitized), limit)
	}
	return truncateDebugString(string(data), limit)
}

// normalizeDebugValue 先把 typed slice/map 归一成通用 JSON 结构，确保嵌套字段也能走统一脱敏逻辑。
func normalizeDebugValue(value interface{}) interface{} {
	if value == nil {
		return nil
	}

	data, err := json.Marshal(value)
	if err != nil {
		return value
	}
	var normalized interface{}
	if err := json.Unmarshal(data, &normalized); err != nil {
		return value
	}
	return normalized
}

func truncateDebugString(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(value[cut]) {
		cut--
	}
	return fmt.Sprintf("%s... [truncated, %d bytes omitted]", value[:cut], len(value)-cut)
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

	// ak/sk/pwd/sign 等短词只做精确匹配，避免误伤 TaskId、DiskId 这类普通字段。
	for _, token := range []string{"ak", "sk", "pwd", "sts", "sign"} {
		if normalized == token || compact == token {
			return true
		}
	}
	for _, token := range []string{
		"access_key",
		"accesskey",
		"api_key",
		"apikey",
		"secret",
		"token",
		"authorization",
		"bearer",
		"credential",
		"password",
		"passwd",
		"private_key",
		"privatekey",
		"signature",
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

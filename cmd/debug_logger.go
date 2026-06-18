package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	envCLIDebug = "VOLCENGINE_CLI_DEBUG"

	defaultDebugValueLimit = 64 * 1024
	maskedDebugValue       = "***MASKED***"
)

type debugOptions struct {
	Enabled bool
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

func newDebugLogger(opts debugOptions) (*DebugLogger, error) {
	if !opts.Enabled {
		return &DebugLogger{enabled: false}, nil
	}

	logFile, err := defaultDebugLogFile()
	if err != nil {
		return nil, err
	}
	file, err := openDebugLogFile(logFile)
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

func defaultDebugLogFile() (string, error) {
	configDir, err := configFileDirFunc()
	if err != nil {
		return "", err
	}
	logsDir := filepath.Join(configDir, "logs")
	if err := os.MkdirAll(logsDir, 0700); err != nil {
		return "", err
	}
	_ = os.Chmod(logsDir, 0700)
	return filepath.Join(logsDir, time.Now().Format("2006010215")+".log"), nil
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
	return validateDebugLogFileInfo(path, info, nil)
}

func verifyOpenedDebugLogFile(path string, file *os.File) error {
	pathInfo, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if err := validateDebugLogFileInfo(path, pathInfo, nil); err != nil {
		return err
	}
	fileInfo, err := file.Stat()
	if err != nil {
		return err
	}
	if !os.SameFile(pathInfo, fileInfo) {
		return fmt.Errorf("debug log file changed while opening: %s", path)
	}
	if err := validateDebugLogFileInfo(path, fileInfo, file); err != nil {
		return err
	}
	return nil
}

func validateDebugLogFileInfo(path string, info os.FileInfo, file *os.File) error {
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("debug log file must not be a symbolic link: %s", path)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("debug log file must be a regular file: %s", path)
	}
	count, err := hardLinkCount(info, file)
	if err != nil {
		return fmt.Errorf("inspect debug log hard links: %w", err)
	}
	if count > 1 {
		return fmt.Errorf("debug log file must not have multiple hard links: %s", path)
	}
	return nil
}

func resolveDebugOptions(ctx *Context) (debugOptions, error) {
	var opts debugOptions

	if raw, ok := os.LookupEnv(envCLIDebug); ok {
		opts.Enabled = parseDebugEnv(raw)
	}

	return opts, nil
}

func parseDebugEnv(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "false", "0", "f", "no", "n", "off":
		return false
	default:
		return true
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

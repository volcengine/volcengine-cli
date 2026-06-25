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

// Enabled 返回 debug logger 当前是否可写。
// nil logger 或显式关闭 debug 时都视为未启用，方便调用方直接在链路中做空值保护。
func (l *DebugLogger) Enabled() bool {
	return l != nil && l.enabled
}

// Printf 在 debug logger 启用时写入一行格式化日志。
// 写入失败只记录首个错误并留到 Close 返回，避免 debug 日志问题打断主业务流程。
func (l *DebugLogger) Printf(format string, args ...interface{}) {
	if !l.Enabled() || l.out == nil {
		return
	}
	if _, err := fmt.Fprintf(l.out, "[debug] "+format+"\n", args...); err != nil && l.err == nil {
		l.err = err
	}
}

// Close 刷新缓冲区并关闭底层输出资源。
// 返回写入、刷新或关闭阶段遇到的首个错误；nil logger 直接返回 nil，便于调用方安全 defer。
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

// newDebugLogger 根据解析后的 debug 配置创建日志器。
// debug 未启用时返回一个禁用态 logger；启用时会创建默认日志文件并使用缓冲写入降低频繁日志输出的 IO 成本。
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

// defaultDebugLogFile 返回当前小时对应的默认 debug 日志文件路径。
// 日志目录固定放在 CLI 配置目录的 logs 子目录下，并尽量收紧为 0700，降低敏感 debug 内容被其他用户读取的风险。
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

// openDebugLogFile 以追加模式打开 debug 日志文件，并在真正写入前后都校验路径。
// 前后两次校验用于规避 TOCTOU 风险，避免 debug 内容被追加到 symlink/hardlink 指向的非预期文件。
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

// rejectUnsafeDebugLogPath 在打开日志文件前检查目标路径是否安全。
// 文件不存在时允许后续创建；若已存在，则要求它是单链接普通文件，避免跟随链接写入敏感内容。
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

// verifyOpenedDebugLogFile 校验已经打开的日志文件仍然对应原始路径。
// 它同时比较路径状态和文件句柄状态，防止文件在打开过程中被替换成链接或其它非预期文件。
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

// validateDebugLogFileInfo 校验日志文件的基础安全属性。
// debug 日志可能包含请求参数和响应内容，因此必须拒绝符号链接、非普通文件和多硬链接文件。
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

// resolveDebugOptions 只负责从当前进程环境解析 debug 配置。
// 这里不接收 Context，避免调用方误以为 debug 开关还会受运行上下文影响。
func resolveDebugOptions() (debugOptions, error) {
	var opts debugOptions

	if raw, ok := os.LookupEnv(envCLIDebug); ok {
		opts.Enabled = parseDebugEnv(raw)
	}

	return opts, nil
}

// parseDebugEnv 将环境变量文本解析为 debug 开关。
// 只有常见的空值和否定值会关闭 debug，其它非空值都视为开启，兼容用户用 true/1/on 之外的自定义标记。
func parseDebugEnv(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "false", "0", "f", "no", "n", "off":
		return false
	default:
		return true
	}
}

// formatDebugValue 将任意 debug 值转换为可写入日志的字符串。
// 转换过程会先做结构归一和敏感字段脱敏，再按字节上限截断，避免日志文件过大或泄露凭证信息。
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

// normalizeDebugValue 先把 typed slice/map 归一成通用 JSON 结构。
// 归一化后嵌套字段也能走统一脱敏逻辑；若值无法 JSON 编解码，则保留原值，避免 debug 日志影响主流程。
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

// truncateDebugString 按字节上限截断 debug 字符串。
// 截断时会回退到 UTF-8 字符边界，避免日志中出现半个多字节字符导致后续查看工具乱码。
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

// sanitizeDebugValue 递归脱敏 debug 值中的敏感字段。
// 当前覆盖通用 map、字符串 map、HTTP Header 和已归一化的数组，未知类型保持原样以减少对业务对象的侵入。
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

// sanitizeDebugMap 复制并脱敏 map 中的敏感字段。
// 返回新 map 而不是原地修改，避免 debug 日志处理意外改变调用方仍在使用的数据结构。
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

// isSensitiveDebugKey 判断字段名是否可能包含凭证、令牌或签名等敏感信息。
// 判断时同时使用下划线规范化和紧凑形式，兼容 access_key、AccessKey、private-key 等不同命名风格。
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
		"secret-key",
		"secretkey",
		"access_token",
		"accesstoken",
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

// debugLoggerFromContext 从 Context 中取出可用的 debug logger。
// 若 Context 为空、logger 未初始化或 debug 未启用，则返回 nil，让调用方可以直接跳过日志输出。
func debugLoggerFromContext(ctx *Context) *DebugLogger {
	if ctx == nil || ctx.debugLogger == nil || !ctx.debugLogger.Enabled() {
		return nil
	}
	return ctx.debugLogger
}

// invocation.go 存放正常路径与 force 路径共用的“调用前”处理：
// context 重置、参数解析、API 版本 / method 固定参数、call-style 解析、
// service 级 action 分发。真正发 SDK 请求的 executeInvocation 仍在 cmd_action.go。
package cmd

import (
	"fmt"
	"strings"
)

// resetInvocationContext 清空单次调用的 flag 集合。
// ctx 为进程级全局变量，每次解析参数前必须重置，避免多次调用 flag 残留。
func resetInvocationContext() {
	if ctx == nil {
		return
	}
	ctx.fixedFlags = NewFlagSet()
	ctx.dynamicFlags = NewFlagSet()
}

// parseInvocationArgs 重置 context 后解析参数，返回位置参数（如 action 名）。
// 系统参数（--profile/--region/... 或三横线别名）进入 fixedFlags；
// 双横线 API 参数进入 dynamicFlags。actionParameters 用于精确冲突消解。
// 预处理剥离的 system flags（主要是 --lang）在 reset 后重新写回 fixedFlags。
func parseInvocationArgs(args []string, actionParameters ...map[string]struct{}) ([]string, error) {
	resetInvocationContext()
	if err := applyResolvedSystemFlags(ctx, processLanguageResolution.fixedFlags); err != nil {
		return nil, err
	}
	parser := NewParser(args, actionParameters...)
	return parser.ReadArgs(ctx)
}

// parseInvocationFlags 与 parseInvocationArgs 相同，但只关心解析错误、丢弃位置参数。
// 用于已知 action 子命令（位置参数已由 cobra 消费）的 flag 解析。
func parseInvocationFlags(args []string, actionParameters ...map[string]struct{}) error {
	_, err := parseInvocationArgs(args, actionParameters...)
	return err
}

// apiVersionForCall 返回本次调用使用的 API 版本：优先 ---version，否则回落到内置元数据。
func apiVersionForCall(c *Context, serviceName string) string {
	if v := explicitAPIVersion(c); v != "" {
		return v
	}
	return rootSupport.GetVersion(serviceName)
}

// explicitAPIVersion 读取 ---version，即 OpenAPI 接口版本（写入 SDK ClientInfo.Version）。
func explicitAPIVersion(c *Context) string {
	if c == nil || c.fixedFlags == nil {
		return ""
	}
	f := c.fixedFlags.GetByName("version")
	if f == nil {
		return ""
	}
	return strings.TrimSpace(f.GetValue())
}

// explicitHTTPMethod 读取显式指定的 ---method，仅允许 GET/POST；空串表示未指定、由元数据或默认值决定。
func explicitHTTPMethod(c *Context) (string, error) {
	if c == nil || c.fixedFlags == nil {
		return "", nil
	}
	f := c.fixedFlags.GetByName("method")
	if f == nil {
		return "", nil
	}
	method := strings.ToUpper(strings.TrimSpace(f.GetValue()))
	if method == "" {
		return "", nil
	}
	if method != "GET" && method != "POST" {
		return "", fmt.Errorf("--method value %q is not supported, please set method in {GET|POST}", method)
	}
	return method, nil
}

// resolveCallStyle 决定 Method / ContentType / 自定义 headers：
//   - method：显式 ---method > 元数据 > GET
//   - contentType：--header Content-Type > 元数据 > 有 --body 时默认 application/json
//   - headers：解析并校验一次后返回，供 CallSdk 注入
//
// 正常路径与 force 路径共用，避免两套 contentType / header 组装分叉。
func resolveCallStyle(ctx *Context, serviceName, action string) (method, contentType string, headers []requestHeader, err error) {
	apiInfo := rootSupport.GetApiInfo(serviceName, action)
	method, err = resolveActionHTTPMethod(ctx, apiInfo)
	if err != nil {
		return "", "", nil, err
	}
	headers, err = collectRequestHeaders(ctx)
	if err != nil {
		return "", "", nil, err
	}
	ct, err := contentTypeFromHeaders(headers)
	if err != nil {
		return "", "", nil, err
	}
	if ct != "" {
		contentType = ct
	} else if apiInfo != nil && apiInfo.ContentType != "" {
		contentType = apiInfo.ContentType
	} else if hasDynamicBodyFlag(ctx) {
		// Unlisted / no-meta force calls often only pass --body; treat as JSON.
		contentType = "application/json"
	}
	return method, contentType, headers, nil
}

// requestHeader is one --header Name=Value pair after parsing.
type requestHeader struct {
	Name  string
	Value string
}

// blockedHTTPHeaderNames cannot be set via --header: they conflict with transport
// or signing. Comparison is case-insensitive.
var blockedHTTPHeaderNames = map[string]struct{}{
	"host":           {},
	"authorization":  {},
	"content-length": {},
}

// collectRequestHeaders parses all --header Name=Value assignments in order.
// Invalid or blocked entries fail the call early.
func collectRequestHeaders(c *Context) ([]requestHeader, error) {
	if c == nil || c.dynamicFlags == nil {
		return nil, nil
	}
	f := c.dynamicFlags.GetByName("header")
	if f == nil {
		return nil, nil
	}
	raw := f.GetValues()
	out := make([]requestHeader, 0, len(raw))
	for _, v := range raw {
		name, value, err := parseHeaderKV(v)
		if err != nil {
			return nil, err
		}
		if _, blocked := blockedHTTPHeaderNames[strings.ToLower(name)]; blocked {
			return nil, fmt.Errorf("--header %q is not allowed (reserved for transport/signing)", name)
		}
		out = append(out, requestHeader{Name: name, Value: value})
	}
	return out, nil
}

// parseHeaderKV splits "Name=Value" on the first '=' (aliyun-cli compatible).
func parseHeaderKV(s string) (name, value string, err error) {
	s = strings.TrimSpace(s)
	idx := strings.Index(s, "=")
	if idx <= 0 {
		return "", "", fmt.Errorf("invalid --header %q, expected HeaderName=Value", s)
	}
	name = strings.TrimSpace(s[:idx])
	value = strings.TrimSpace(s[idx+1:])
	if name == "" {
		return "", "", fmt.Errorf("invalid --header %q, expected HeaderName=Value", s)
	}
	return name, value, nil
}

// contentTypeFromHeaders returns the last Content-Type value among custom headers
// (case-insensitive). An explicit empty Content-Type value is an error.
func contentTypeFromHeaders(headers []requestHeader) (string, error) {
	var (
		found bool
		ct    string
	)
	for _, h := range headers {
		if strings.EqualFold(h.Name, "Content-Type") {
			found = true
			ct = h.Value
		}
	}
	if found && ct == "" {
		return "", fmt.Errorf("--header Content-Type value must not be empty")
	}
	return ct, nil
}

// mediaType returns the type/subtype of a Content-Type value (lowercased),
// stripping parameters after ';' (e.g. "application/json; charset=utf-8").
func mediaType(contentType string) string {
	s := strings.TrimSpace(strings.ToLower(contentType))
	if i := strings.Index(s, ";"); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	return s
}

// isJSONContentType reports whether contentType is application/json, optionally
// with parameters (charset, etc.).
func isJSONContentType(contentType string) bool {
	return mediaType(contentType) == "application/json"
}

// hasDynamicBodyFlag reports whether --body was provided among dynamic flags.
func hasDynamicBodyFlag(c *Context) bool {
	if c == nil || c.dynamicFlags == nil {
		return false
	}
	f := c.dynamicFlags.GetByName("body")
	return f != nil && strings.TrimSpace(f.GetValue()) != ""
}

// dispatchServiceAction 统一 service 级 action 分发：force 优先，否则已知 action 走正常路径。
func dispatchServiceAction(ctx *Context, svc, action string, known bool) error {
	if isForceEnabled(ctx) {
		return doForceAction(ctx, svc, action)
	}
	if known {
		return doAction(ctx, svc, action)
	}
	return fmt.Errorf("%q is not a supported action of %q", action, svc)
}

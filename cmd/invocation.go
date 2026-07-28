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
// 固定三横线 flag 进入 fixedFlags，双横线 API 参数进入 dynamicFlags。
func parseInvocationArgs(args []string) ([]string, error) {
	resetInvocationContext()
	parser := NewParser(args)
	return parser.ReadArgs(ctx)
}

// parseInvocationFlags 与 parseInvocationArgs 相同，但只关心解析错误、丢弃位置参数。
// 用于已知 action 子命令（位置参数已由 cobra 消费）的 flag 解析。
func parseInvocationFlags(args []string) error {
	_, err := parseInvocationArgs(args)
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
		return "", fmt.Errorf("---method value %q is not supported, please set method in {GET|POST}", method)
	}
	return method, nil
}

// resolveCallStyle 决定调用的 Method/ContentType：method 走 resolveActionHTTPMethod；
// contentType 优先 ---content-type，其次元数据，最后在无元数据且存在 --body 时默认 application/json。
// 正常路径与 force 路径共用，避免两套 contentType 组装分叉。
func resolveCallStyle(ctx *Context, serviceName, action string) (method, contentType string, err error) {
	apiInfo := rootSupport.GetApiInfo(serviceName, action)
	method, err = resolveActionHTTPMethod(ctx, apiInfo)
	if err != nil {
		return "", "", err
	}
	if ct := explicitContentType(ctx); ct != "" {
		contentType = ct
	} else if apiInfo != nil && apiInfo.ContentType != "" {
		contentType = apiInfo.ContentType
	} else if hasDynamicBodyFlag(ctx) {
		// Unlisted / no-meta force calls often only pass --body; treat as JSON.
		contentType = "application/json"
	}
	return method, contentType, nil
}

// explicitContentType 读取 ---content-type（HTTP 请求 Content-Type 覆盖）。
func explicitContentType(c *Context) string {
	if c == nil || c.fixedFlags == nil {
		return ""
	}
	f := c.fixedFlags.GetByName("content-type")
	if f == nil {
		return ""
	}
	return strings.TrimSpace(f.GetValue())
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

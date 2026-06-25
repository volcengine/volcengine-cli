package cmd

import (
	"strings"
	"time"

	"github.com/volcengine/volcengine-go-sdk/volcengine/client"
	"github.com/volcengine/volcengine-go-sdk/volcengine/request"
	"github.com/volcengine/volcengine-go-sdk/volcengine/volcengineerr"
)

// addDebugRequestAttemptHandler 为 SDK Client 注册每次请求尝试完成后的 debug 日志回调。
// 只有 debug logger 已启用时才注册，避免在正常请求路径上增加不必要的 handler 和日志开销。
func (s *SdkClient) addDebugRequestAttemptHandler(c *client.Client) {
	if s == nil || c == nil || s.DebugLogger == nil || !s.DebugLogger.Enabled() {
		return
	}
	logger := s.DebugLogger
	// CompleteAttempt 会在每一次 HTTP 尝试结束时触发，包括后续重试。
	// 调试排障时需要拿到每次尝试的 request id，而不是只记录最终 SDK 调用结果。
	c.Handlers.CompleteAttempt.PushBackNamed(request.NamedHandler{
		Name: "volcengine-cli.debug.request-attempt",
		Fn: func(r *request.Request) {
			debugLogSDKRequestAttempt(logger, r)
		},
	})
}

// debugLogSDKRequestAttempt 记录单次 SDK HTTP 尝试的关键排障信息。
// 这里按 attempt 维度记录状态码、request id、重试次数、耗时和错误，便于定位重试链路中的具体失败点。
func debugLogSDKRequestAttempt(logger *DebugLogger, r *request.Request) {
	if logger == nil || !logger.Enabled() || r == nil {
		return
	}
	statusCode := 0
	if r.HTTPResponse != nil {
		statusCode = r.HTTPResponse.StatusCode
	}
	duration := time.Duration(0)
	if !r.AttemptTime.IsZero() {
		duration = time.Since(r.AttemptTime)
	}
	logger.Printf("sdk_request_attempt service=%s action=%s method=%s status_code=%d request_id=%s retry_count=%d duration_ms=%d error=%s",
		debugRequestService(r),
		debugRequestAction(r),
		debugRequestMethod(r),
		statusCode,
		debugRequestID(r),
		r.RetryCount,
		duration/time.Millisecond,
		debugRequestError(r),
	)
}

// debugRequestService 从 SDK Request 中读取服务名。
// Request 为空时返回空字符串，保证 debug 日志路径不会因为排障信息缺失而影响主流程。
func debugRequestService(r *request.Request) string {
	if r == nil {
		return ""
	}
	return r.ClientInfo.ServiceName
}

// debugRequestAction 从 SDK Operation 中读取接口动作名。
// 部分失败发生在 Operation 初始化前，因此需要同时兼容 Request 或 Operation 为空的情况。
func debugRequestAction(r *request.Request) string {
	if r == nil || r.Operation == nil {
		return ""
	}
	return r.Operation.Name
}

// debugRequestMethod 返回本次请求使用的 HTTP 方法。
// 优先使用 Operation 上声明的方法；若 Operation 缺失，则退回到实际构造出的 HTTPRequest 方法。
func debugRequestMethod(r *request.Request) string {
	if r == nil {
		return ""
	}
	if r.Operation != nil && r.Operation.HTTPMethod != "" {
		return r.Operation.HTTPMethod
	}
	if r.HTTPRequest != nil {
		return r.HTTPRequest.Method
	}
	return ""
}

// debugRequestError 返回压缩成单行的 SDK 请求错误信息。
// 多行错误会让一条 attempt 日志拆成多行，影响按 request_id 或时间线检索，因此这里统一折叠空白字符。
func debugRequestError(r *request.Request) string {
	if r == nil || r.Error == nil {
		return ""
	}
	// SDK 的 RequestFailure 文案可能包含换行和缩进；压成单行可以避免 debug log
	// 被一条错误拆成多行，后续按 request_id 检索时更稳定。
	return strings.Join(strings.Fields(r.Error.Error()), " ")
}

// debugRequestID 尽可能从 SDK Request 的不同位置提取 request id。
// 不同协议、响应形态和失败阶段暴露 request id 的位置不一致，因此这里按可靠性和常见程度逐级兜底。
func debugRequestID(r *request.Request) string {
	if r == nil {
		return ""
	}
	// 不同协议和不同失败阶段暴露 request id 的位置不一致：
	// - 服务端错误通常在 RequestFailure 里；
	// - 结构化响应可能写入 r.Metadata；
	// - CLI 通用调用使用 map 输出时，SDK 不会填 r.Metadata，需要从响应体 map 里读；
	// - 少数接口只在 header 中返回日志 ID。
	if requestID := debugRequestFailureID(r.Error); requestID != "" {
		return requestID
	}
	if r.Metadata.RequestId != "" {
		return r.Metadata.RequestId
	}
	if requestID := debugRequestDataID(r.Data); requestID != "" {
		return requestID
	}
	if r.RequestID != "" {
		return r.RequestID
	}
	if r.HTTPResponse != nil {
		for _, header := range []string{"X-Tt-Logid", "X-Request-Id", "X-Request-ID"} {
			if requestID := r.HTTPResponse.Header.Get(header); requestID != "" {
				return requestID
			}
		}
	}
	return ""
}

// debugRequestFailureID 从 SDK RequestFailure 错误中读取 request id。
// 服务端返回错误时 request id 通常随错误对象一起暴露，这是失败排障时最直接的来源。
func debugRequestFailureID(err error) string {
	if err == nil {
		return ""
	}
	if failure, ok := err.(volcengineerr.RequestFailure); ok {
		return failure.RequestID()
	}
	return ""
}

// debugRequestDataID 从 SDK 响应数据对象中读取 request id。
// CLI 通用调用常把响应反序列化成 map，SDK Metadata 可能不会被填充，因此需要从 Data 里额外兜底。
func debugRequestDataID(data interface{}) string {
	switch v := data.(type) {
	case map[string]interface{}:
		return debugRequestMetadataID(v)
	case *map[string]interface{}:
		if v == nil {
			return ""
		}
		return debugRequestMetadataID(*v)
	default:
		return ""
	}
}

// debugRequestMetadataID 从响应体的 ResponseMetadata 字段中读取 request id。
// 不同接口和协议可能使用 RequestId、RequestID、request_id 或 requestId，统一兼容这些常见命名。
func debugRequestMetadataID(data map[string]interface{}) string {
	raw, ok := data["ResponseMetadata"]
	if !ok {
		return ""
	}
	metadata, ok := raw.(map[string]interface{})
	if !ok {
		return ""
	}
	for _, key := range []string{"RequestId", "RequestID", "request_id", "requestId"} {
		if requestID, ok := metadata[key].(string); ok && requestID != "" {
			return requestID
		}
	}
	return ""
}

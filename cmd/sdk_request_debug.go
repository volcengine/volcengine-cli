package cmd

import (
	"strings"
	"time"

	"github.com/volcengine/volcengine-go-sdk/volcengine/client"
	"github.com/volcengine/volcengine-go-sdk/volcengine/request"
	"github.com/volcengine/volcengine-go-sdk/volcengine/volcengineerr"
)

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

func debugRequestService(r *request.Request) string {
	if r == nil {
		return ""
	}
	return r.ClientInfo.ServiceName
}

func debugRequestAction(r *request.Request) string {
	if r == nil || r.Operation == nil {
		return ""
	}
	return r.Operation.Name
}

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

func debugRequestError(r *request.Request) string {
	if r == nil || r.Error == nil {
		return ""
	}
	// SDK 的 RequestFailure 文案可能包含换行和缩进；压成单行可以避免 debug log
	// 被一条错误拆成多行，后续按 request_id 检索时更稳定。
	return strings.Join(strings.Fields(r.Error.Error()), " ")
}

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

func debugRequestFailureID(err error) string {
	if err == nil {
		return ""
	}
	if failure, ok := err.(volcengineerr.RequestFailure); ok {
		return failure.RequestID()
	}
	return ""
}

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

package cmd

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
)

// generateCodeVerifier 生成一个符合 RFC 7636 规范的 code_verifier。
// 使用 crypto/rand 生成 43 字节随机数据，再以 base64url（无填充）编码，
// 结果长度为 43 字符，仅包含 unreserved URI 字符 [A-Za-z0-9\-._~]。
// 使用 crypto/rand 生成 32 字节随机数据，再以 base64url（无填充）编码，
// 结果长度为 43 字符（RFC 7636 要求 43~128 字符），仅包含 unreserved URI 字符 [A-Za-z0-9\-._~]。
func generateCodeVerifier() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("生成 code_verifier 失败: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// generateCodeChallenge 根据给定的 code_verifier 计算 S256 code_challenge。
// 算法: BASE64URL(SHA256(code_verifier))，不含填充字符。
func generateCodeChallenge(verifier string) string {
	hash := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(hash[:])
}

// generateState 生成一个 UUID v4 格式的随机字符串，用作 OAuth 请求的 state 参数。
func generateState() string {
	var uuid [16]byte
	// 忽略错误：crypto/rand.Read 在主流平台上不会失败。
	_, _ = rand.Read(uuid[:])

	// 设置 UUID v4 版本位和变体位。
	uuid[6] = (uuid[6] & 0x0f) | 0x40 // version 4
	uuid[8] = (uuid[8] & 0x3f) | 0x80 // variant 10

	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		uuid[0:4],
		uuid[4:6],
		uuid[6:8],
		uuid[8:10],
		uuid[10:16],
	)
}

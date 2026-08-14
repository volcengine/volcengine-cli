package upgrade

// Copyright 2022 Beijing Volcanoengine Technology Ltd.  All Rights Reserved.

import (
	"path/filepath"
	"strings"
)

// defaultDetectors 有序检测器：先 Homebrew/Linuxbrew，再 npm。
// 先判 brew，避免误对 Cellar 内二进制做原地覆盖。
var defaultDetectors = []Detector{
	homebrewPathDetector{},
	npmPathDetector{},
}

// normalizeInstallPath 规范化路径供匹配：统一为正斜杠并转小写。
// 任意 GOOS 下都会把 '\' 换成 '/'，保证 Windows 风格路径在测试与边界情况下也能匹配。
func normalizeInstallPath(p string) string {
	p = strings.ReplaceAll(p, "\\", "/")
	p = filepath.Clean(p)
	p = filepath.ToSlash(p)
	// Windows 上 Clean 可能再次引入 '\'，再规范化一次
	p = strings.ReplaceAll(p, "\\", "/")
	return strings.ToLower(p)
}

// pathHasSegment 判断规范化路径中是否包含完整路径段 seg（不是其它名字的子串）。
// 例如 seg=homebrew 能匹配 /opt/homebrew/bin/ve，不会匹配 /opt/not-homebrew/ve。
func pathHasSegment(normalizedPath, seg string) bool {
	if normalizedPath == "" || seg == "" {
		return false
	}
	seg = strings.ToLower(seg)
	if normalizedPath == seg {
		return true
	}
	if strings.HasPrefix(normalizedPath, seg+"/") {
		return true
	}
	if strings.HasSuffix(normalizedPath, "/"+seg) {
		return true
	}
	return strings.Contains(normalizedPath, "/"+seg+"/")
}

// homebrewPathDetector 识别 macOS Homebrew 与 Linux 上 Homebrew/Linuxbrew 布局。
// 按路径段匹配，覆盖常见前缀：/opt/homebrew、Cellar、linuxbrew、.linuxbrew 等。
type homebrewPathDetector struct{}

func (homebrewPathDetector) Method() Method { return MethodHomebrew }

func (homebrewPathDetector) Match(normalizedPath string) bool {
	if normalizedPath == "" {
		return false
	}
	// 先匹配 linuxbrew（路径中可能同时出现 homebrew 字样）
	if pathHasSegment(normalizedPath, "linuxbrew") {
		return true
	}
	// 点目录形态：/.linuxbrew/
	if strings.Contains(normalizedPath, "/.linuxbrew/") ||
		strings.HasSuffix(normalizedPath, "/.linuxbrew") ||
		strings.HasPrefix(normalizedPath, ".linuxbrew/") {
		return true
	}
	if pathHasSegment(normalizedPath, "homebrew") {
		return true
	}
	// Cellar 为 Homebrew 按版本存放公式产物的目录
	if pathHasSegment(normalizedPath, "cellar") {
		return true
	}
	return false
}

func (homebrewPathDetector) Info(execPath string) InstallInfo {
	return homebrewInfo(execPath)
}

// npmPathDetector 识别官方 npm 包 @volcengine/cli 的安装路径。
type npmPathDetector struct{}

func (npmPathDetector) Method() Method { return MethodNPM }

func (npmPathDetector) Match(normalizedPath string) bool {
	if normalizedPath == "" {
		return false
	}
	// 规范化后统一为正斜杠，Windows / Unix 布局均可匹配
	return strings.Contains(normalizedPath, "/node_modules/@volcengine/cli/")
}

func (npmPathDetector) Info(execPath string) InstallInfo {
	return npmInfo(execPath)
}

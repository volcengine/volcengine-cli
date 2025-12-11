package util

// Copyright 2022 Beijing Volcanoengine Technology Ltd.  All Rights Reserved.

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"os/user"
	"runtime"
	"strings"
	"time"
)

func IsRepeatedField(f string) bool {
	return strings.Contains(f, ".N")
}

func IsJsonArray(value string) bool {
	return len(value) >= 2 && value[0] == '[' && value[len(value)-1] == ']'
}

// ParseToJsonArrayOrObject try to parse string to json array or json object
func ParseToJsonArrayOrObject(s string) (interface{}, bool) {
	if !json.Valid([]byte(s)) || len(s) < 2 {
		return nil, false
	}

	var a interface{}
	if (s[0] == '[' && s[len(s)-1] == ']') || (s[0] == '{' && s[len(s)-1] == '}') {
		if err := json.Unmarshal([]byte(s), &a); err != nil {
			return err, false
		} else {
			return a, true
		}
	}
	return nil, false
}

func GetConfigFileDir() (string, error) {
	var (
		err     error
		homeDir string
	)

	if homeDir, err = getHomeDir(); err != nil {
		return "", err
	}

	return homeDir + "/.volcengine/", nil
}

func getHomeDir() (string, error) {
	user, err := user.Current()
	if err != nil {
		return "", err
	}

	return user.HomeDir, nil
}

// OpenBrowser 打开浏览器
func OpenBrowser(url string) error {
	switch runtime.GOOS {
	case "linux":
		return exec.Command("xdg-open", url).Start()
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	case "darwin":
		return exec.Command("open", url).Start()
	default:
		return fmt.Errorf("无法自动打开浏览器，请手动访问")
	}
}

func UnixTimestampToTime(ts int64) time.Time {
	switch {
	case ts >= 1e18: // 纳秒
		return time.Unix(0, ts)
	case ts >= 1e15: // 微秒
		return time.Unix(0, ts*int64(time.Microsecond))
	case ts >= 1e12: // 毫秒
		sec := ts / 1000
		nsec := (ts % 1000) * int64(time.Millisecond)
		return time.Unix(sec, nsec)
	default: // 秒
		return time.Unix(ts, 0)
	}
}

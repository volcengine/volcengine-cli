//go:build !windows
// +build !windows

package cmd

import (
	"os"
	"reflect"
)

// hardLinkCount 从 Unix-like 平台的 os.FileInfo.Sys() 中读取 Nlink。
// 找不到字段时返回 0，保持旧平台或特殊文件系统上的兼容行为。
func hardLinkCount(info os.FileInfo, _ *os.File) (uint64, error) {
	if info == nil || info.Sys() == nil {
		return 0, nil
	}
	value := reflect.Indirect(reflect.ValueOf(info.Sys()))
	if !value.IsValid() {
		return 0, nil
	}
	field := value.FieldByName("Nlink")
	if !field.IsValid() {
		return 0, nil
	}
	switch field.Kind() {
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return field.Uint(), nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if field.Int() > 0 {
			return uint64(field.Int()), nil
		}
	}
	return 0, nil
}

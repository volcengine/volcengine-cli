//go:build windows
// +build windows

package cmd

import (
	"os"
	"syscall"
)

// hardLinkCount 在 Windows 上必须通过已打开的文件句柄读取 NumberOfLinks。
// os.FileInfo.Sys() 只暴露 Win32FileAttributeData，没有硬链接数量；如果继续读 Nlink，
// 校验会退化成永远返回 0，导致 hard-linked debug log 文件被错误接受。
func hardLinkCount(_ os.FileInfo, file *os.File) (uint64, error) {
	if file == nil {
		return 0, nil
	}
	var data syscall.ByHandleFileInformation
	if err := syscall.GetFileInformationByHandle(syscall.Handle(file.Fd()), &data); err != nil {
		return 0, err
	}
	return uint64(data.NumberOfLinks), nil
}

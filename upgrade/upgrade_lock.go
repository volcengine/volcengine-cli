package upgrade

// Copyright 2022 Beijing Volcanoengine Technology Ltd.  All Rights Reserved.

import (
	"fmt"
	"os"
)

type upgradeLock struct {
	file *os.File
}

func acquireUpgradeLock(currentPath string) (*upgradeLock, error) {
	lockPath := currentPath + ".upgrade.lock"
	file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("failed to open upgrade lock %s: %v", lockPath, err)
	}
	if err := tryLockUpgradeFile(file); err != nil {
		_ = file.Close()
		if isUpgradeLockBusy(err) {
			return nil, fmt.Errorf("another upgrade is already in progress for %s", currentPath)
		}
		return nil, fmt.Errorf("failed to lock upgrade target %s: %v", currentPath, err)
	}
	return &upgradeLock{file: file}, nil
}

func (l *upgradeLock) release() {
	if l == nil || l.file == nil {
		return
	}
	_ = unlockUpgradeFile(l.file)
	_ = l.file.Close()
}

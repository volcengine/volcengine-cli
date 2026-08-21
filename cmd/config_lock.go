package cmd

import "os"

type configFileLock struct {
	file *os.File
}

// acquireConfigFileLock opens a stable sibling lock file. The file is never
// unlinked: removing a held lock path can create two independently locked files.
func acquireConfigFileLock(path string) (*configFileLock, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := file.Chmod(0600); err != nil {
		_ = file.Close()
		return nil, err
	}
	if err := lockConfigFile(file); err != nil {
		_ = file.Close()
		return nil, err
	}
	return &configFileLock{file: file}, nil
}

func (lock *configFileLock) release() error {
	if lock == nil || lock.file == nil {
		return nil
	}
	unlockErr := unlockConfigFile(lock.file)
	closeErr := lock.file.Close()
	if unlockErr != nil {
		return unlockErr
	}
	return closeErr
}

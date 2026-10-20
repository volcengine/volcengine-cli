//go:build windows
// +build windows

package scriptrun

import (
	"fmt"
	"os/exec"
	"unsafe"

	"golang.org/x/sys/windows"
)

// processTree owns a Job Object configured to kill every process in the job
// when it is terminated or its last handle is closed. The worker is assigned
// before the broker sends the start frame, so user code cannot start
// descendants outside the job before assignment.
type processTree struct {
	job windows.Handle
}

func prepareProcessTree(command *exec.Cmd) {
	command.SysProcAttr = &windows.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP}
}

func attachProcessTree(command *exec.Cmd) (*processTree, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, fmt.Errorf("create job object: %w", err)
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
		BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{
			LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE,
		},
	}
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		_ = windows.CloseHandle(job)
		return nil, fmt.Errorf("configure job object: %w", err)
	}
	process, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(command.Process.Pid))
	if err != nil {
		_ = windows.CloseHandle(job)
		return nil, fmt.Errorf("open worker process: %w", err)
	}
	defer windows.CloseHandle(process)
	if err := windows.AssignProcessToJobObject(job, process); err != nil {
		_ = windows.CloseHandle(job)
		return nil, fmt.Errorf("assign worker to job object: %w", err)
	}
	return &processTree{job: job}, nil
}

func (tree *processTree) kill() {
	if tree == nil || tree.job == 0 {
		return
	}
	_ = windows.TerminateJobObject(tree.job, 1)
}

func (tree *processTree) close() {
	if tree == nil || tree.job == 0 {
		return
	}
	_ = windows.CloseHandle(tree.job)
	tree.job = 0
}

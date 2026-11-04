package claude

import (
	"fmt"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const claudeProgram = "claude.exe"

// startProcess attaches the suspended child to a job before it can spawn descendants.
// Closing the last job handle also cleans up when the TUI's console is closed.
func startProcess(cmd *exec.Cmd) (func(), error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, fmt.Errorf("create process job: %w", err)
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		windows.CloseHandle(job)
		return nil, fmt.Errorf("configure process job: %w", err)
	}
	var mu sync.Mutex
	cleanup := func() {
		mu.Lock()
		defer mu.Unlock()
		if job != 0 {
			windows.CloseHandle(job)
			job = 0
		}
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW | windows.CREATE_SUSPENDED}
	cmd.WaitDelay = time.Second
	cmd.Cancel = func() error {
		mu.Lock()
		defer mu.Unlock()
		if job == 0 {
			return os.ErrProcessDone
		}
		return windows.TerminateJobObject(job, 1)
	}
	// Cancellation must wait until assignment; otherwise it could terminate an empty job.
	mu.Lock()
	err = cmd.Start()
	if err == nil {
		err = attachProcess(job, uint32(cmd.Process.Pid))
	}
	mu.Unlock()
	if err != nil {
		if cmd.Process != nil {
			cmd.Process.Kill()
			cmd.Wait()
		}
		cleanup()
		return nil, err
	}
	return cleanup, nil
}

func attachProcess(job windows.Handle, pid uint32) error {
	process, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, pid)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(process)
	if err := windows.AssignProcessToJobObject(job, process); err != nil {
		return fmt.Errorf("assign process job: %w", err)
	}
	// os/exec closes the primary thread handle. The suspended process has one thread;
	// reopen it through the documented Tool Help API to resume after job assignment.
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPTHREAD, 0)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(snapshot)
	entry := windows.ThreadEntry32{}
	entry.Size = uint32(unsafe.Sizeof(entry))
	for err := windows.Thread32First(snapshot, &entry); err == nil; err = windows.Thread32Next(snapshot, &entry) {
		if entry.OwnerProcessID != pid {
			continue
		}
		thread, err := windows.OpenThread(windows.THREAD_SUSPEND_RESUME, false, entry.ThreadID)
		if err != nil {
			return err
		}
		defer windows.CloseHandle(thread)
		_, err = windows.ResumeThread(thread)
		return err
	}
	return fmt.Errorf("primary thread for process %d was not found", pid)
}

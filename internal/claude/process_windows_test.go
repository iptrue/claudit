package claude

import (
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

func helperWatchHangup() {}

func assertProcessIsolated(t *testing.T, pid int) {
	t.Helper()
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_INFORMATION, false, uint32(pid))
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(handle)
	var inJob int32
	result, _, err := windows.NewLazySystemDLL("kernel32.dll").NewProc("IsProcessInJob").Call(uintptr(handle), 0, uintptr(unsafe.Pointer(&inJob)))
	if result == 0 || inJob == 0 {
		t.Errorf("process %d belongs to no job: result=%d, %v", pid, result, err)
	}
}

func processExists(pid int) bool {
	handle, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(handle)
	status, err := windows.WaitForSingleObject(handle, 0)
	return err == nil && status == uint32(windows.WAIT_TIMEOUT)
}

func killTestProcess(pid int) {
	handle, err := windows.OpenProcess(windows.PROCESS_TERMINATE, false, uint32(pid))
	if err == nil {
		defer windows.CloseHandle(handle)
		windows.TerminateProcess(handle, 1)
	}
}

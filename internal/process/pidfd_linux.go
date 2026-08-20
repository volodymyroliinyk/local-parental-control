//go:build linux && (amd64 || arm64)

package process

import "syscall"

const (
	sysPIDFDOpen       = 434
	sysPIDFDSendSignal = 424
)

func openPIDFD(pid int) (int, error) {
	fd, _, errno := syscall.Syscall(sysPIDFDOpen, uintptr(pid), 0, 0)
	if errno != 0 {
		return -1, errno
	}
	return int(fd), nil
}

func signalPIDFD(pidfd int, signal syscall.Signal) error {
	_, _, errno := syscall.Syscall6(sysPIDFDSendSignal, uintptr(pidfd), uintptr(signal), 0, 0, 0, 0)
	if errno != 0 {
		return errno
	}
	return nil
}

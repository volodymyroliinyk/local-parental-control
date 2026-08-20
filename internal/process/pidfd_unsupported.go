//go:build !linux || (!amd64 && !arm64)

package process

import "syscall"

func openPIDFD(_ int) (int, error)              { return -1, syscall.ENOSYS }
func signalPIDFD(_ int, _ syscall.Signal) error { return syscall.ENOSYS }

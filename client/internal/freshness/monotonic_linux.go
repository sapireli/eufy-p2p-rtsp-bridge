//go:build linux

package freshness

import (
	"syscall"
	"unsafe"
)

func monotonicNs() uint64 {
	var t syscall.Timespec
	_, _, e := syscall.Syscall(syscall.SYS_CLOCK_GETTIME, 1, uintptr(unsafe.Pointer(&t)), 0)
	if e != 0 {
		panic(e)
	}
	return uint64(t.Sec)*1000000000 + uint64(t.Nsec)
}

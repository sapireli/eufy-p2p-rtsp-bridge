//go:build linux

package drmbackground

import (
	"fmt"
	"os"
	"runtime"
	"syscall"
	"unsafe"
)

type linuxDevice struct{ fd int }

func nativeDevice(file *os.File) (device, error) {
	switch runtime.GOARCH {
	case "arm", "arm64", "amd64", "386":
		return linuxDevice{int(file.Fd())}, nil
	default:
		return nil, fmt.Errorf("DRM background: unsupported ioctl ABI %s", runtime.GOARCH)
	}
}

func (d linuxDevice) ioctl(op uint8, arg []byte, arrays ...array) error {
	var pin runtime.Pinner
	defer pin.Unpin()
	pin.Pin(&arg[0])
	for _, a := range arrays {
		if len(a.data) == 0 {
			put64(arg, a.offset, 0)
			continue
		}
		pin.Pin(&a.data[0])
		put64(arg, a.offset, uint64(uintptr(unsafe.Pointer(&a.data[0]))))
	}
	for {
		_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(d.fd), ioctlRequest(op, len(arg)), uintptr(unsafe.Pointer(&arg[0])))
		if errno == syscall.EINTR {
			continue
		}
		if errno != 0 {
			return errno
		}
		return nil
	}
}

func (d linuxDevice) mmap(offset uint64, size int) ([]byte, error) {
	if offset > uint64(1<<63-1) {
		return nil, fmt.Errorf("DRM mmap offset exceeds int64")
	}
	return syscall.Mmap(d.fd, int64(offset), size, syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_SHARED)
}

func (d linuxDevice) munmap(mapping []byte) error { return syscall.Munmap(mapping) }

//go:build linux

package tun

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

const (
	IFF_TUN   = 0x0001
	IFF_NO_PI = 0x1000
)

type ifreq struct {
	ifrName  [16]byte
	ifrFlags int16
	_        [22]byte
}

type linuxTun struct {
	file *os.File
	name string
	mtu  int
}

// CreateAllocates a new persistent or transient TUN interface on Linux.
func Create(name string, mtu int) (Device, error) {
	file, err := os.OpenFile("/dev/net/tun", os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("tun: failed to open /dev/net/tun: %w", err)
	}

	var req ifreq
	req.ifrFlags = IFF_TUN | IFF_NO_PI
	copy(req.ifrName[:], []byte(name))

	_, _, errno := syscall.Syscall(
		syscall.SYS_IOCTL,
		file.Fd(),
		uintptr(0x400454ca), // TUNSETIFF
		uintptr(unsafe.Pointer(&req)),
	)
	if errno != 0 {
		file.Close()
		return nil, fmt.Errorf("tun: ioctl TUNSETIFF failed: %w", errno)
	}

	actualName := string(req.ifrName[:])
	for i, b := range req.ifrName {
		if b == 0 {
			actualName = string(req.ifrName[:i])
			break
		}
	}

	if mtu <= 0 {
		mtu = 1280
	}

	return &linuxTun{
		file: file,
		name: actualName,
		mtu:  mtu,
	}, nil
}

func (t *linuxTun) Read(buf []byte) (int, error) {
	return t.file.Read(buf)
}

func (t *linuxTun) Write(buf []byte) (int, error) {
	return t.file.Write(buf)
}

func (t *linuxTun) Close() error {
	return t.file.Close()
}

func (t *linuxTun) Name() string {
	return t.name
}

func (t *linuxTun) MTU() int {
	return t.mtu
}

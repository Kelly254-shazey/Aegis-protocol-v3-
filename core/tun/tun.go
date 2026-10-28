package tun

import (
	"errors"
	"io"
)

var (
	ErrTunClosed = errors.New("tun: device is closed")
)

// Device represents an OS-level virtual Layer 3 network interface.
type Device interface {
	io.ReadWriteCloser
	Name() string
	MTU() int
}

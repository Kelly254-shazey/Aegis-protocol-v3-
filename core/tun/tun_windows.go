//go:build windows

package tun

import (
	"sync"
)

type windowsSimulatedTun struct {
	mu     sync.Mutex
	name   string
	mtu    int
	queue  chan []byte
	closed bool
}

// Create on Windows creates an in-memory packet buffer device for testing and SOCKS5 bridge integration.
func Create(name string, mtu int) (Device, error) {
	if mtu <= 0 {
		mtu = 1280
	}
	return &windowsSimulatedTun{
		name:  name,
		mtu:   mtu,
		queue: make(chan []byte, 1024),
	}, nil
}

func (t *windowsSimulatedTun) Read(buf []byte) (int, error) {
	pkt, ok := <-t.queue
	if !ok {
		return 0, ErrTunClosed
	}
	n := copy(buf, pkt)
	return n, nil
}

func (t *windowsSimulatedTun) Write(buf []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return 0, ErrTunClosed
	}
	cp := make([]byte, len(buf))
	copy(cp, buf)
	select {
	case t.queue <- cp:
		return len(buf), nil
	default:
		return 0, nil // Drop if queue saturated
	}
}

func (t *windowsSimulatedTun) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.closed {
		t.closed = true
		close(t.queue)
	}
	return nil
}

func (t *windowsSimulatedTun) Name() string {
	return t.name
}

func (t *windowsSimulatedTun) MTU() int {
	return t.mtu
}

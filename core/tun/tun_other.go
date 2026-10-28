//go:build !linux && !windows

package tun

import (
	"sync"
)

type genericSimulatedTun struct {
	mu     sync.Mutex
	name   string
	mtu    int
	queue  chan []byte
	closed bool
}

// Create on other Unix-like systems creates an in-memory packet buffer device.
func Create(name string, mtu int) (Device, error) {
	if mtu <= 0 {
		mtu = 1280
	}
	return &genericSimulatedTun{
		name:  name,
		mtu:   mtu,
		queue: make(chan []byte, 1024),
	}, nil
}

func (t *genericSimulatedTun) Read(buf []byte) (int, error) {
	pkt, ok := <-t.queue
	if !ok {
		return 0, ErrTunClosed
	}
	n := copy(buf, pkt)
	return n, nil
}

func (t *genericSimulatedTun) Write(buf []byte) (int, error) {
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
		return 0, nil
	}
}

func (t *genericSimulatedTun) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.closed {
		t.closed = true
		close(t.queue)
	}
	return nil
}

func (t *genericSimulatedTun) Name() string {
	return t.name
}

func (t *genericSimulatedTun) MTU() int {
	return t.mtu
}

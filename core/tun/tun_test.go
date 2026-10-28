package tun

import (
	"bytes"
	"testing"
	"time"
)

func TestTunDeviceLifecycle(t *testing.T) {
	dev, err := Create("aegis_test0", 1280)
	if err != nil {
		t.Fatalf("Failed to create TUN device: %v", err)
	}
	defer dev.Close()

	if dev.MTU() != 1280 {
		t.Fatalf("Expected MTU 1280, got %d", dev.MTU())
	}

	testPayload := []byte{0x45, 0x00, 0x00, 0x28, 0x12, 0x34, 0x40, 0x00, 0x40, 0x06, 0x00, 0x00}

	// Write packet to TUN
	n, err := dev.Write(testPayload)
	if err != nil {
		t.Fatalf("Failed to write to TUN device: %v", err)
	}
	if n != len(testPayload) {
		t.Fatalf("Expected %d bytes written, got %d", len(testPayload), n)
	}

	// Read packet from TUN
	readBuf := make([]byte, 1500)
	readChan := make(chan int, 1)

	go func() {
		rn, rErr := dev.Read(readBuf)
		if rErr == nil {
			readChan <- rn
		} else {
			readChan <- -1
		}
	}()

	select {
	case rn := <-readChan:
		if rn != len(testPayload) {
			t.Fatalf("Expected %d bytes read, got %d", len(testPayload), rn)
		}
		if !bytes.Equal(readBuf[:rn], testPayload) {
			t.Fatalf("Read payload does not match written payload")
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("Timeout waiting for TUN packet read")
	}

	// Close device and verify ErrTunClosed
	if err := dev.Close(); err != nil {
		t.Fatalf("Failed to close TUN device: %v", err)
	}

	_, writeErr := dev.Write(testPayload)
	if writeErr == nil {
		t.Fatalf("Expected error writing to closed TUN device, got nil")
	}
}

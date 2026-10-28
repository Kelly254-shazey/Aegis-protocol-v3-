package crypto

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"
)

func TestAuth_VerifyValidTicket(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate ed25519 key: %v", err)
	}

	ticket := SessionTicket{
		UserID:     "user-1001",
		DeviceID:   "device-android-alpha",
		Tier:       "duo",
		MaxDevices: 2,
		IssuedAt:   time.Now().Unix(),
		ExpiresAt:  time.Now().Add(24 * time.Hour).Unix(),
	}

	payload, err := json.Marshal(ticket)
	if err != nil {
		t.Fatalf("failed to marshal ticket: %v", err)
	}

	sig := ed25519.Sign(priv, payload)
	sigB64 := base64.StdEncoding.EncodeToString(sig)

	st := &SignedTicket{
		Ticket:    ticket,
		Signature: sigB64,
	}

	verified, err := Verify(st, pub)
	if err != nil {
		t.Fatalf("expected valid ticket, got error: %v", err)
	}

	if verified.UserID != "user-1001" {
		t.Errorf("mismatched user ID: got %s, want user-1001", verified.UserID)
	}

	// Test Access Key formatting
	key, err := st.ToAccessKey()
	if err != nil {
		t.Fatalf("failed to generate access key: %v", err)
	}
	parsedST, err := ParseAccessKey(key)
	if err != nil {
		t.Fatalf("failed to parse access key: %v", err)
	}
	if parsedST.Ticket.UserID != "user-1001" {
		t.Fatalf("mismatched parsed user ID: %s", parsedST.Ticket.UserID)
	}
}

func TestAuth_RejectExpiredTicket(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)

	ticket := SessionTicket{
		UserID:     "user-expired",
		DeviceID:   "device-0",
		Tier:       "solo",
		MaxDevices: 1,
		IssuedAt:   time.Now().Add(-48 * time.Hour).Unix(),
		ExpiresAt:  time.Now().Add(-24 * time.Hour).Unix(), // Expired yesterday
	}

	payload, _ := json.Marshal(ticket)
	sig := ed25519.Sign(priv, payload)

	st := &SignedTicket{
		Ticket:    ticket,
		Signature: base64.StdEncoding.EncodeToString(sig),
	}

	_, err := Verify(st, pub)
	if err != ErrTokenExpired {
		t.Fatalf("expected ErrTokenExpired, got: %v", err)
	}
}

func TestAuth_RejectTamperedTicket(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)

	ticket := SessionTicket{
		UserID:     "legit-user",
		DeviceID:   "device-1",
		Tier:       "solo",
		MaxDevices: 1,
		IssuedAt:   time.Now().Unix(),
		ExpiresAt:  time.Now().Add(1 * time.Hour).Unix(),
	}

	payload, _ := json.Marshal(ticket)
	sig := ed25519.Sign(priv, payload)

	// Attacker modifies tier to "home"
	tamperedTicket := ticket
	tamperedTicket.Tier = "home"

	st := &SignedTicket{
		Ticket:    tamperedTicket,
		Signature: base64.StdEncoding.EncodeToString(sig),
	}

	_, err := Verify(st, pub)
	if err != ErrInvalidSignature {
		t.Fatalf("expected ErrInvalidSignature for tampered ticket, got: %v", err)
	}
}

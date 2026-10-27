package crypto

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

var (
	ErrTokenExpired    = errors.New("auth: session token has expired")
	ErrInvalidSignature = errors.New("auth: cryptographic signature mismatch")
	ErrMalformedToken  = errors.New("auth: malformed token payload")
)

// SessionTicket is the signed Ed25519 pass issued by the Customer Activation Portal.
type SessionTicket struct {
	UserID     string   `json:"uid"`
	DeviceID   string   `json:"did"`
	Tier       string   `json:"tier"` // e.g. "solo", "duo", "home"
	MaxDevices int      `json:"max_dev"`
	IssuedAt   int64    `json:"iat"`
	ExpiresAt  int64    `json:"exp"`
}

// SignedTicket wraps a SessionTicket with its Ed25519 signature.
type SignedTicket struct {
	Ticket    SessionTicket `json:"t"`
	Signature string        `json:"sig"` // Base64-encoded 64-byte Ed25519 signature
}

// Verify checks the validity and expiration of a ticket in memory without touching a database.
func Verify(st *SignedTicket, portalPubKey ed25519.PublicKey) (*SessionTicket, error) {
	if st == nil {
		return nil, ErrMalformedToken
	}

	// 1. Check expiration
	now := time.Now().Unix()
	if now > st.Ticket.ExpiresAt {
		return nil, ErrTokenExpired
	}

	// 2. Serialize ticket canonical JSON for signature verification
	payload, err := json.Marshal(st.Ticket)
	if err != nil {
		return nil, fmt.Errorf("auth: json marshal error: %w", err)
	}

	sigBytes, err := base64.StdEncoding.DecodeString(st.Signature)
	if err != nil || len(sigBytes) != ed25519.SignatureSize {
		return nil, ErrInvalidSignature
	}

	// 3. Verify asymmetric signature against portal's public key (takes <1 microsecond)
	if !ed25519.Verify(portalPubKey, payload, sigBytes) {
		return nil, ErrInvalidSignature
	}

	return &st.Ticket, nil
}

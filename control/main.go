package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"aegis-core/crypto"
)

type ControlServer struct {
	mu            sync.RWMutex
	portalPrivKey ed25519.PrivateKey
	portalPubKey  ed25519.PublicKey
	challenges    map[string]int64 // challenge -> expiry timestamp
}

func NewControlServer() (*ControlServer, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	cs := &ControlServer{
		portalPrivKey: priv,
		portalPubKey:  pub,
		challenges:    make(map[string]int64),
	}
	go cs.gcChallenges()
	return cs, nil
}

// HandleMPesaSTKPush initiates mobile money payment for East Africa
func (cs *ControlServer) HandleMPesaSTKPush(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		PhoneNumber string `json:"phone_number"` // e.g. 254712345678
		Tier        string `json:"tier"`         // "solo", "duo", "home"
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid payload", http.StatusBadRequest)
		return
	}

	maxDev := 1
	if req.Tier == "duo" {
		maxDev = 2
	} else if req.Tier == "home" {
		maxDev = 5
	}

	// Issue Cryptographic Pass (30-day validity)
	ticket := crypto.SessionTicket{
		UserID:     "user_" + req.PhoneNumber,
		DeviceID:   "dev_primary",
		Tier:       req.Tier,
		MaxDevices: maxDev,
		IssuedAt:   time.Now().Unix(),
		ExpiresAt:  time.Now().Add(30 * 24 * time.Hour).Unix(),
	}

	ticketData, _ := json.Marshal(ticket)
	sig := ed25519.Sign(cs.portalPrivKey, ticketData)

	signedPass := crypto.SignedTicket{
		Ticket:    ticket,
		Signature: base64.StdEncoding.EncodeToString(sig),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"status":  "activated",
		"message": "Shield pass issued. Ready to connect.",
		"pass":    signedPass,
		"pubKey":  base64.StdEncoding.EncodeToString(cs.portalPubKey),
	})
}

// HandlePairingQR generates a 120-second challenge for laptop pairing (Duo tier)
func (cs *ControlServer) HandlePairingQR(w http.ResponseWriter, r *http.Request) {
	challenge := make([]byte, 32)
	_, _ = rand.Read(challenge)
	challengeB64 := base64.StdEncoding.EncodeToString(challenge)

	cs.mu.Lock()
	cs.challenges[challengeB64] = time.Now().Add(120 * time.Second).Unix()
	cs.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"challenge": challengeB64,
		"expiresIn": "120s",
		"qrPayload": fmt.Sprintf("aegis://pair?c=%s&ts=%d", challengeB64, time.Now().Unix()),
	})
}

// HandleStatus returns service health
func (cs *ControlServer) HandleStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"status":    "healthy",
		"version":   "3.0.0",
		"timestamp": time.Now().Unix(),
	})
}

func (cs *ControlServer) gcChallenges() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		cs.mu.Lock()
		now := time.Now().Unix()
		for c, exp := range cs.challenges {
			if now > exp {
				delete(cs.challenges, c)
			}
		}
		cs.mu.Unlock()
	}
}

func main() {
	port := flag.String("port", ":8080", "Control Plane HTTP Port")
	flag.Parse()

	cs, err := NewControlServer()
	if err != nil {
		log.Fatalf("Failed to initialize Control Server: %v", err)
	}

	http.HandleFunc("/api/v3/pay/mpesa", cs.HandleMPesaSTKPush)
	http.HandleFunc("/api/v3/device/pair-qr", cs.HandlePairingQR)
	http.HandleFunc("/api/v3/status", cs.HandleStatus)

	log.Printf("🛡️ Aegis Protocol v3 Control Plane running on %s", *port)
	if err := http.ListenAndServe(*port, nil); err != nil {
		log.Fatal(err)
	}
}

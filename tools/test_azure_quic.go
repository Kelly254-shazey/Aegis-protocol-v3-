package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"time"

	"github.com/quic-go/quic-go"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	tlsConf := &tls.Config{
		InsecureSkipVerify: true,
		NextProtos:         []string{"aegis-h3"},
	}
	quicConf := &quic.Config{
		EnableDatagrams: true,
	}

	fmt.Println("Dialing QUIC to 172.209.217.140:51820...")
	conn, err := quic.DialAddr(ctx, "172.209.217.140:51820", tlsConf, quicConf)
	if err != nil {
		fmt.Printf("QUIC Dial FAILED: %v\n", err)
		return
	}
	defer conn.CloseWithError(0, "")
	fmt.Printf("🎉 QUIC Dial SUCCEEDED! Remote: %v\n", conn.RemoteAddr())
}

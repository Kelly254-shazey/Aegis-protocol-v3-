//go:build android && cgo

package main

/*
#include <jni.h>
#include <stdlib.h>

static const char* jni_get_string(JNIEnv* env, jstring str) {
    return (*env)->GetStringUTFChars(env, str, NULL);
}

static void jni_release_string(JNIEnv* env, jstring str, const char* chars) {
    (*env)->ReleaseStringUTFChars(env, str, chars);
}
*/
import "C"
import (
	"context"
	"log"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"aegis-core/fec"
	"aegis-core/telemetry"
	"aegis-core/transport"
)

var (
	engineMu     sync.Mutex
	activeEngine *ProductionEngine
)

type ProductionEngine struct {
	client     *transport.Client
	fecEnc     *fec.Encoder
	fecDec     *fec.Decoder
	adaptive   *fec.AdaptiveController
	telemetry  *telemetry.Manager
	tunFile    *os.File
	packetConn net.PacketConn
	ctx        context.Context
	cancel     context.CancelFunc
	running    atomic.Bool
}

//export Java_com_aegis_v3_AegisCore_startEngineNative
func Java_com_aegis_v3_AegisCore_startEngineNative(
	env *C.JNIEnv,
	clazz C.jclass,
	jEndpoint C.jstring,
	jServerName C.jstring,
	tunFd C.jint,
	socketFd C.jint,
) C.jint {
	engineMu.Lock()
	defer engineMu.Unlock()

	if activeEngine != nil && activeEngine.running.Load() {
		return 0
	}

	cEndpoint := C.jni_get_string(env, jEndpoint)
	cServerName := C.jni_get_string(env, jServerName)
	endpoint := C.GoString(cEndpoint)
	serverName := C.GoString(cServerName)
	C.jni_release_string(env, jEndpoint, cEndpoint)
	C.jni_release_string(env, jServerName, cServerName)

	enc, err := fec.NewEncoder(10, 1)
	if err != nil {
		log.Printf("[Aegis-JNI] Failed to init FEC encoder: %v", err)
		return -1
	}

	// 1. Wrap protected UDP socket from Android VpnService
	var packetConn net.PacketConn
	if socketFd > 0 {
		file := os.NewFile(uintptr(socketFd), "udp-protect")
		var err error
		packetConn, err = net.FilePacketConn(file)
		if err != nil {
			log.Printf("[Aegis-JNI] net.FilePacketConn failed: %v", err)
			packetConn = nil
		}
	}
	if packetConn == nil {
		var err error
		packetConn, err = net.ListenUDP("udp", &net.UDPAddr{Port: 0})
		if err != nil {
			log.Printf("[Aegis-JNI] net.ListenUDP fallback failed: %v", err)
			return -2
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	engine := &ProductionEngine{
		client:     transport.NewClient(endpoint, serverName),
		fecEnc:     enc,
		adaptive:   fec.NewAdaptiveController(enc),
		telemetry:  telemetry.NewManager(),
		tunFile:    os.NewFile(uintptr(tunFd), "tun0"),
		packetConn: packetConn,
		ctx:        ctx,
		cancel:     cancel,
	}

	// In dev/self-signed mode, allow peer verification bypass
	engine.client.SetInsecureSkipVerify(true)

	// 2. Connect QUIC transport over protected UDP socket
	dialCtx, dialCancel := context.WithTimeout(ctx, 5*time.Second)
	defer dialCancel()

	if err := engine.client.Connect(dialCtx, packetConn); err != nil {
		log.Printf("[Aegis-JNI] Transport Connect to %s failed: %v", endpoint, err)
		cancel()
		_ = packetConn.Close()
		return -3
	}

	log.Printf("[Aegis-JNI] Connected successfully to Cloud Edge Relay %s", endpoint)

	// 3. Initialize Inbound FEC Decoder
	engine.fecDec = fec.NewDecoder(3*time.Second, func(blockID uint32, packets [][]byte) {
		for _, pkt := range packets {
			if len(pkt) > 0 && engine.tunFile != nil {
				_, _ = engine.tunFile.Write(pkt)
			}
		}
	})

	engine.running.Store(true)
	activeEngine = engine

	// 4. Inbound Datagram Loop (QUIC -> FEC Decoder -> tun0)
	go func() {
		datagrams := engine.client.Datagrams()
		for engine.running.Load() {
			select {
			case <-engine.ctx.Done():
				return
			case data, ok := <-datagrams:
				if !ok {
					return
				}
				shard, err := fec.DecodeShard(data)
				if err == nil {
					_ = engine.fecDec.Ingest(shard)
				}
			}
		}
	}()

	// 5. Outbound Loop (tun0 -> FEC Encoder -> QUIC Datagrams)
	go func() {
		readChan := make(chan []byte, 64)

		go func() {
			buf := make([]byte, 1500)
			for engine.running.Load() {
				n, err := engine.tunFile.Read(buf)
				if err != nil {
					close(readChan)
					return
				}
				if n > 0 {
					pkt := make([]byte, n)
					copy(pkt, buf[:n])
					select {
					case readChan <- pkt:
					case <-engine.ctx.Done():
						return
					}
				}
			}
		}()

		batch := make([][]byte, 0, 10)
		ticker := time.NewTicker(5 * time.Millisecond)
		defer ticker.Stop()

		flushBatch := func() {
			if len(batch) == 0 {
				return
			}
			shards, err := engine.fecEnc.EncodeBlock(batch)
			if err == nil {
				for _, s := range shards {
					_ = engine.client.SendDatagram(s.Encode())
				}
			}
			batch = batch[:0]
		}

		for engine.running.Load() {
			select {
			case <-engine.ctx.Done():
				return
			case pkt, ok := <-readChan:
				if !ok {
					return
				}
				batch = append(batch, pkt)
				strategy := engine.adaptive.Current()
				if len(batch) >= strategy.DataShards {
					flushBatch()
				}
			case <-ticker.C:
				if len(batch) > 0 {
					flushBatch()
				}
			}
		}
	}()

	return 0
}

//export Java_com_aegis_v3_AegisCore_updateTelemetryNative
func Java_com_aegis_v3_AegisCore_updateTelemetryNative(
	env *C.JNIEnv,
	clazz C.jclass,
	rsrp C.jint,
	rsrq C.jint,
	rssnr C.jint,
	loss C.jdouble,
) {
	engineMu.Lock()
	defer engineMu.Unlock()

	if activeEngine != nil && activeEngine.adaptive != nil {
		activeEngine.adaptive.UpdateMetrics(float64(loss))
	}
}

//export Java_com_aegis_v3_AegisCore_stopEngineNative
func Java_com_aegis_v3_AegisCore_stopEngineNative(env *C.JNIEnv, clazz C.jclass) {
	engineMu.Lock()
	defer engineMu.Unlock()

	if activeEngine != nil {
		activeEngine.running.Store(false)
		activeEngine.cancel()
		if activeEngine.client != nil {
			_ = activeEngine.client.Close()
		}
		if activeEngine.packetConn != nil {
			_ = activeEngine.packetConn.Close()
		}
		if activeEngine.tunFile != nil {
			_ = activeEngine.tunFile.Close()
		}
		activeEngine = nil
	}
}

func main() {}

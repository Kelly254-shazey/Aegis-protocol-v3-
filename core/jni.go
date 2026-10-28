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
	bufferPool   = sync.Pool{
		New: func() any {
			b := make([]byte, 1500)
			return &b
		},
	}
)

type ProductionEngine struct {
	client    *transport.Client
	fecEnc    *fec.Encoder
	fecDec    *fec.Decoder
	adaptive  *fec.AdaptiveController
	telemetry *telemetry.Manager
	tunFile   *os.File
	ctx       context.Context
	cancel    context.CancelFunc
	running   atomic.Bool
}

//export Java_com_aegis_v3_AegisCore_startEngineNative
func Java_com_aegis_v3_AegisCore_startEngineNative(
	env *C.JNIEnv,
	clazz C.jclass,
	jEndpoint C.jstring,
	jServerName C.jstring,
	tunFd C.jint,
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
		return -1
	}

	ctx, cancel := context.WithCancel(context.Background())
	engine := &ProductionEngine{
		client:    transport.NewClient(endpoint, serverName),
		fecEnc:    enc,
		adaptive:  fec.NewAdaptiveController(enc),
		telemetry: telemetry.NewManager(),
		tunFile:   os.NewFile(uintptr(tunFd), "tun0"),
		ctx:       ctx,
		cancel:    cancel,
	}

	engine.fecDec = fec.NewDecoder(3*time.Second, func(blockID uint32, packets [][]byte) {
		for _, pkt := range packets {
			if len(pkt) > 0 && engine.tunFile != nil {
				_, _ = engine.tunFile.Write(pkt)
			}
		}
	})

	engine.running.Store(true)
	activeEngine = engine

	go func() {
		batch := make([][]byte, 0, 10)
		for engine.running.Load() {
			bufPtr := bufferPool.Get().(*[]byte)
			buf := *bufPtr

			n, err := engine.tunFile.Read(buf)
			if err != nil {
				bufferPool.Put(bufPtr)
				return
			}
			if n > 0 {
				packet := make([]byte, n)
				copy(packet, buf[:n])
				batch = append(batch, packet)

				strategy := engine.adaptive.Current()
				if len(batch) >= strategy.DataShards {
					shards, err := engine.fecEnc.EncodeBlock(batch)
					if err == nil {
						for _, s := range shards {
							_ = engine.client.SendDatagram(s.Encode())
						}
					}
					batch = batch[:0]
				}
			}
			bufferPool.Put(bufPtr)
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
		if activeEngine.tunFile != nil {
			_ = activeEngine.tunFile.Close()
		}
		activeEngine = nil
	}
}

func main() {}

package main

/*
#include <stdint.h>
#include <stdlib.h>
*/
import "C"
import (
	"sync"
	"unsafe"

	"aegis-core/fec"
	"aegis-core/telemetry"
	"aegis-core/transport"
)

var (
	globalEngine *EngineWrapper
	globalMu     sync.Mutex
)

type EngineWrapper struct {
	client    *transport.Client
	fecEnc    *fec.Encoder
	fecDec    *fec.Decoder
	adaptive  *fec.AdaptiveController
	telemetry *telemetry.Manager
	tunFd     int
}

//export StartAegisEngine
func StartAegisEngine(cEndpoint *C.char, cServerName *C.char, tunFd C.int) C.int {
	globalMu.Lock()
	defer globalMu.Unlock()

	endpoint := C.GoString(cEndpoint)
	serverName := C.GoString(cServerName)

	enc, err := fec.NewEncoder(10, 1)
	if err != nil {
		return -1
	}

	globalEngine = &EngineWrapper{
		client:    transport.NewClient(endpoint, serverName),
		fecEnc:    enc,
		adaptive:  fec.NewAdaptiveController(enc),
		telemetry: telemetry.NewManager(),
		tunFd:     int(tunFd),
	}

	return 0
}

//export UpdateRadioTelemetry
func UpdateRadioTelemetry(rsrp C.int, rsrq C.int, rssnr C.int, loss C.double) {
	globalMu.Lock()
	defer globalMu.Unlock()

	if globalEngine != nil && globalEngine.adaptive != nil {
		globalEngine.adaptive.UpdateMetrics(float64(loss))
	}
}

//export StopAegisEngine
func StopAegisEngine() {
	globalMu.Lock()
	defer globalMu.Unlock()

	if globalEngine != nil {
		if globalEngine.client != nil {
			_ = globalEngine.client.Close()
		}
		globalEngine = nil
	}
}

func main() {}

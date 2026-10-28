package main

import (
	"encoding/binary"
	"fmt"
	"net"
	"sync"
	"time"
)

// ipv4Checksum computes the 16-bit one's complement sum for IPv4 headers.
func ipv4Checksum(b []byte) uint16 {
	var sum uint32
	for i := 0; i < len(b)-1; i += 2 {
		sum += uint32(binary.BigEndian.Uint16(b[i:]))
	}
	if len(b)%2 == 1 {
		sum += uint32(b[len(b)-1]) << 8
	}
	for sum > 0xffff {
		sum = (sum >> 16) + (sum & 0xffff)
	}
	return ^uint16(sum)
}

// tcpChecksum computes the RFC 793 checksum for TCP segment including IPv4 pseudo-header.
func tcpChecksum(srcIP, dstIP net.IP, segment []byte) uint16 {
	var sum uint32
	src := srcIP.To4()
	dst := dstIP.To4()
	if src == nil || dst == nil {
		return 0
	}

	sum += uint32(binary.BigEndian.Uint16(src[0:2]))
	sum += uint32(binary.BigEndian.Uint16(src[2:4]))
	sum += uint32(binary.BigEndian.Uint16(dst[0:2]))
	sum += uint32(binary.BigEndian.Uint16(dst[2:4]))
	sum += uint32(6) // Protocol TCP
	sum += uint32(len(segment))

	for i := 0; i < len(segment)-1; i += 2 {
		sum += uint32(binary.BigEndian.Uint16(segment[i:]))
	}
	if len(segment)%2 == 1 {
		sum += uint32(segment[len(segment)-1]) << 8
	}
	for sum > 0xffff {
		sum = (sum >> 16) + (sum & 0xffff)
	}
	return ^uint16(sum)
}

// buildIPv4Packet constructs a standard IPv4 packet header around a payload.
func buildIPv4Packet(protocol byte, srcIP, dstIP net.IP, payload []byte) []byte {
	totalLen := 20 + len(payload)
	pkt := make([]byte, totalLen)

	pkt[0] = 0x45 // Version 4, IHL 5 (20 bytes)
	pkt[1] = 0x00 // DSCP / ECN
	binary.BigEndian.PutUint16(pkt[2:4], uint16(totalLen))
	binary.BigEndian.PutUint16(pkt[4:6], uint16(time.Now().UnixNano()&0xffff))
	pkt[6] = 0x40 // Flags: Don't Fragment
	pkt[7] = 0x00
	pkt[8] = 64 // TTL
	pkt[9] = protocol
	pkt[10] = 0 // Checksum placeholder
	pkt[11] = 0
	copy(pkt[12:16], srcIP.To4())
	copy(pkt[16:20], dstIP.To4())

	// Compute IPv4 checksum
	chk := ipv4Checksum(pkt[:20])
	binary.BigEndian.PutUint16(pkt[10:12], chk)

	copy(pkt[20:], payload)
	return pkt
}

// buildUDPResponse builds a complete IPv4 + UDP packet.
func buildUDPResponse(srcIP, dstIP net.IP, srcPort, dstPort uint16, udpPayload []byte) []byte {
	udpLen := 8 + len(udpPayload)
	udpHeaderAndData := make([]byte, udpLen)

	binary.BigEndian.PutUint16(udpHeaderAndData[0:2], srcPort)
	binary.BigEndian.PutUint16(udpHeaderAndData[2:4], dstPort)
	binary.BigEndian.PutUint16(udpHeaderAndData[4:6], uint16(udpLen))
	udpHeaderAndData[6] = 0 // UDP checksum is optional in IPv4 (0x0000 = disabled)
	udpHeaderAndData[7] = 0
	copy(udpHeaderAndData[8:], udpPayload)

	return buildIPv4Packet(17, srcIP, dstIP, udpHeaderAndData)
}

// UserspaceNAT bridges raw IP packets from mobile overlay clients to the WAN.
type UserspaceNAT struct {
	mu           sync.RWMutex
	udpSessions  sync.Map // key: string "clientIP:clientPort->destIP:destPort" -> *udpNatEntry
	tcpSessions  sync.Map // key: string "clientIP:clientPort->destIP:destPort" -> *tcpNatEntry
	onDownlink   func(destIP string, rawIPv4Packet []byte)
	closed       bool
}

type udpNatEntry struct {
	conn       *net.UDPConn
	lastActive time.Time
}

type tcpNatEntry struct {
	mu          sync.Mutex
	clientIP    net.IP
	destIP      net.IP
	clientPort  uint16
	destPort    uint16
	clientSeq   uint32
	relaySeq    uint32
	realConn    net.Conn
	established bool
	closed      bool
	lastActive  time.Time
}

func NewUserspaceNAT(onDownlink func(destIP string, rawIPv4Packet []byte)) *UserspaceNAT {
	nat := &UserspaceNAT{
		onDownlink: onDownlink,
	}
	go nat.gcLoop()
	return nat
}

func (n *UserspaceNAT) Close() {
	n.mu.Lock()
	n.closed = true
	n.mu.Unlock()

	n.udpSessions.Range(func(key, value any) bool {
		entry := value.(*udpNatEntry)
		_ = entry.conn.Close()
		return true
	})
	n.tcpSessions.Range(func(key, value any) bool {
		entry := value.(*tcpNatEntry)
		entry.mu.Lock()
		if entry.realConn != nil {
			_ = entry.realConn.Close()
		}
		entry.closed = true
		entry.mu.Unlock()
		return true
	})
}

// IngestPacket classifies and handles an inbound IPv4 packet from a mobile client.
func (n *UserspaceNAT) IngestPacket(pkt []byte) {
	if len(pkt) < 20 || (pkt[0]>>4) != 4 {
		return
	}

	ihl := int(pkt[0]&0x0F) * 4
	if len(pkt) < ihl {
		return
	}

	proto := pkt[9]
	srcIP := net.IP(pkt[12:16])
	dstIP := net.IP(pkt[16:20])
	payload := pkt[ihl:]

	switch proto {
	case 1: // ICMP
		n.handleICMP(srcIP, dstIP, payload)
	case 17: // UDP
		n.handleUDP(srcIP, dstIP, payload)
	case 6: // TCP
		n.handleTCP(srcIP, dstIP, payload)
	}
}

// handleICMP replies directly to Echo Requests (ping)
func (n *UserspaceNAT) handleICMP(srcIP, dstIP net.IP, payload []byte) {
	if len(payload) < 8 {
		return
	}
	icmpType := payload[0]
	if icmpType == 8 { // Echo Request
		reply := make([]byte, len(payload))
		copy(reply, payload)
		reply[0] = 0 // Echo Reply
		reply[2] = 0 // Checksum reset
		reply[3] = 0

		// Recalculate ICMP checksum
		chk := ipv4Checksum(reply)
		binary.BigEndian.PutUint16(reply[2:4], chk)

		// Build return IPv4 packet (swap src and dst)
		responsePkt := buildIPv4Packet(1, dstIP, srcIP, reply)
		n.onDownlink(srcIP.String(), responsePkt)
	}
}

// handleUDP forwards UDP payloads (such as DNS queries) to the real destination.
func (n *UserspaceNAT) handleUDP(srcIP, dstIP net.IP, payload []byte) {
	if len(payload) < 8 {
		return
	}

	srcPort := binary.BigEndian.Uint16(payload[0:2])
	dstPort := binary.BigEndian.Uint16(payload[2:4])
	udpLen := int(binary.BigEndian.Uint16(payload[4:6]))
	if len(payload) < udpLen || udpLen < 8 {
		return
	}
	udpData := payload[8:udpLen]

	sessionKey := fmt.Sprintf("%s:%d->%s:%d", srcIP.String(), srcPort, dstIP.String(), dstPort)
	val, ok := n.udpSessions.Load(sessionKey)

	var entry *udpNatEntry
	if !ok {
		destAddr := &net.UDPAddr{IP: dstIP, Port: int(dstPort)}
		conn, err := net.DialUDP("udp", nil, destAddr)
		if err != nil {
			return
		}

		entry = &udpNatEntry{
			conn:       conn,
			lastActive: time.Now(),
		}
		n.udpSessions.Store(sessionKey, entry)

		// Background listener for responses from internet
		go func(c *net.UDPConn, sIP, dIP net.IP, sPort, dPort uint16, key string) {
			buf := make([]byte, 2048)
			for {
				_ = c.SetReadDeadline(time.Now().Add(60 * time.Second))
				nBytes, err := c.Read(buf)
				if err != nil {
					_ = c.Close()
					n.udpSessions.Delete(key)
					return
				}
				if nBytes > 0 {
					respPkt := buildUDPResponse(dIP, sIP, dPort, sPort, buf[:nBytes])
					n.onDownlink(sIP.String(), respPkt)
				}
			}
		}(conn, srcIP, dstIP, srcPort, dstPort, sessionKey)
	} else {
		entry = val.(*udpNatEntry)
		entry.lastActive = time.Now()
	}

	_, _ = entry.conn.Write(udpData)
}

// handleTCP bridges TCP connections in userspace to WAN.
func (n *UserspaceNAT) handleTCP(srcIP, dstIP net.IP, payload []byte) {
	if len(payload) < 20 {
		return
	}

	srcPort := binary.BigEndian.Uint16(payload[0:2])
	dstPort := binary.BigEndian.Uint16(payload[2:4])
	seq := binary.BigEndian.Uint32(payload[4:8])
	ack := binary.BigEndian.Uint32(payload[8:12])
	dataOffset := int(payload[12]>>4) * 4
	if len(payload) < dataOffset {
		return
	}
	flags := payload[13]
	tcpData := payload[dataOffset:]

	sessionKey := fmt.Sprintf("%s:%d->%s:%d", srcIP.String(), srcPort, dstIP.String(), dstPort)

	// Check flags
	isSyn := (flags & 0x02) != 0
	isAck := (flags & 0x10) != 0
	isFin := (flags & 0x01) != 0
	isRst := (flags & 0x04) != 0

	if isRst {
		if val, ok := n.tcpSessions.LoadAndDelete(sessionKey); ok {
			entry := val.(*tcpNatEntry)
			entry.mu.Lock()
			if entry.realConn != nil {
				_ = entry.realConn.Close()
			}
			entry.closed = true
			entry.mu.Unlock()
		}
		return
	}

	if isSyn && !isAck {
		// New TCP connection request: dial outbound WAN
		destAddr := fmt.Sprintf("%s:%d", dstIP.String(), dstPort)
		realConn, err := net.DialTimeout("tcp", destAddr, 5*time.Second)
		if err != nil {
			// Send RST-ACK to client
			n.sendTCP(dstIP, srcIP, dstPort, srcPort, 0, seq+1, 0x14, nil)
			return
		}

		relayInitialSeq := uint32(1000000 + (time.Now().UnixNano() & 0x00FFFFFF))
		entry := &tcpNatEntry{
			clientIP:    srcIP,
			destIP:      dstIP,
			clientPort:  srcPort,
			destPort:    dstPort,
			clientSeq:   seq + 1,
			relaySeq:    relayInitialSeq + 1,
			realConn:    realConn,
			established: false,
			lastActive:  time.Now(),
		}
		n.tcpSessions.Store(sessionKey, entry)

		// Send SYN-ACK back to client
		n.sendTCP(dstIP, srcIP, dstPort, srcPort, relayInitialSeq, seq+1, 0x12, nil)

		// Background reader for data from real WAN server
		go n.tcpReaderLoop(entry, sessionKey)
		return
	}

	val, ok := n.tcpSessions.Load(sessionKey)
	if !ok {
		// Unknown TCP session, send RST
		n.sendTCP(dstIP, srcIP, dstPort, srcPort, ack, seq+1, 0x14, nil)
		return
	}

	entry := val.(*tcpNatEntry)
	entry.mu.Lock()
	defer entry.mu.Unlock()
	entry.lastActive = time.Now()

	if isFin {
		if entry.realConn != nil {
			_ = entry.realConn.Close()
		}
		entry.closed = true
		n.tcpSessions.Delete(sessionKey)
		// Send FIN-ACK
		n.sendTCP(dstIP, srcIP, dstPort, srcPort, entry.relaySeq, seq+1, 0x11, nil)
		return
	}

	if isAck && !entry.established {
		entry.established = true
	}

	if len(tcpData) > 0 && entry.realConn != nil {
		_, err := entry.realConn.Write(tcpData)
		if err != nil {
			_ = entry.realConn.Close()
			entry.closed = true
			n.tcpSessions.Delete(sessionKey)
			return
		}
		entry.clientSeq = seq + uint32(len(tcpData))
		// Send ACK back for the received data
		n.sendTCP(dstIP, srcIP, dstPort, srcPort, entry.relaySeq, entry.clientSeq, 0x10, nil)
	}
}

func (n *UserspaceNAT) tcpReaderLoop(entry *tcpNatEntry, sessionKey string) {
	buf := make([]byte, 1200) // MTU safe chunk
	for {
		entry.mu.Lock()
		if entry.closed || entry.realConn == nil {
			entry.mu.Unlock()
			return
		}
		conn := entry.realConn
		entry.mu.Unlock()

		_ = conn.SetReadDeadline(time.Now().Add(120 * time.Second))
		nBytes, err := conn.Read(buf)
		if err != nil {
			entry.mu.Lock()
			if !entry.closed {
				entry.closed = true
				_ = entry.realConn.Close()
				n.sendTCP(entry.destIP, entry.clientIP, entry.destPort, entry.clientPort, entry.relaySeq, entry.clientSeq, 0x11, nil) // FIN-ACK
			}
			entry.mu.Unlock()
			n.tcpSessions.Delete(sessionKey)
			return
		}

		if nBytes > 0 {
			entry.mu.Lock()
			currentRelaySeq := entry.relaySeq
			currentClientSeq := entry.clientSeq
			entry.relaySeq += uint32(nBytes)
			entry.lastActive = time.Now()
			entry.mu.Unlock()

			// Send data chunk to client
			n.sendTCP(entry.destIP, entry.clientIP, entry.destPort, entry.clientPort, currentRelaySeq, currentClientSeq, 0x18, buf[:nBytes]) // PSH-ACK
		}
	}
}

// sendTCP crafts and emits an IPv4 + TCP packet to downlink.
func (n *UserspaceNAT) sendTCP(srcIP, dstIP net.IP, srcPort, dstPort uint16, seq, ack uint32, flags byte, data []byte) {
	tcpLen := 20 + len(data)
	tcpBuf := make([]byte, tcpLen)

	binary.BigEndian.PutUint16(tcpBuf[0:2], srcPort)
	binary.BigEndian.PutUint16(tcpBuf[2:4], dstPort)
	binary.BigEndian.PutUint32(tcpBuf[4:8], seq)
	binary.BigEndian.PutUint32(tcpBuf[8:12], ack)
	tcpBuf[12] = 0x50 // Data offset 5 (20 bytes), reserved 0
	tcpBuf[13] = flags
	binary.BigEndian.PutUint16(tcpBuf[14:16], 65535) // Window size
	tcpBuf[16] = 0                                   // Checksum
	tcpBuf[17] = 0
	tcpBuf[18] = 0 // Urgent pointer
	tcpBuf[19] = 0
	copy(tcpBuf[20:], data)

	// Calculate TCP Checksum
	chk := tcpChecksum(srcIP, dstIP, tcpBuf)
	binary.BigEndian.PutUint16(tcpBuf[16:18], chk)

	// Build IPv4 wrapper
	rawIPv4 := buildIPv4Packet(6, srcIP, dstIP, tcpBuf)
	n.onDownlink(dstIP.String(), rawIPv4)
}

// gcLoop prunes stale NAT sessions periodically.
func (n *UserspaceNAT) gcLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		now := time.Now()
		n.udpSessions.Range(func(key, val any) bool {
			entry := val.(*udpNatEntry)
			if now.Sub(entry.lastActive) > 60*time.Second {
				_ = entry.conn.Close()
				n.udpSessions.Delete(key)
			}
			return true
		})

		n.tcpSessions.Range(func(key, val any) bool {
			entry := val.(*tcpNatEntry)
			entry.mu.Lock()
			if now.Sub(entry.lastActive) > 180*time.Second {
				if entry.realConn != nil {
					_ = entry.realConn.Close()
				}
				entry.closed = true
				n.tcpSessions.Delete(key)
			}
			entry.mu.Unlock()
			return true
		})
	}
}

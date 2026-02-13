package protocol

import (
	"encoding/binary"
	"fmt"
)

type ControlFrameKind int

const (
	ControlFrameUnknown ControlFrameKind = iota
	ControlFrameProxyAns
	ControlFrameSimpleAck
	ControlFrameCloseExt
)

type ControlFrame struct {
	Kind      ControlFrameKind
	Op        uint32
	Flags     int32
	OutConnID int64
	Confirm   int32
	Payload   []byte
}

type ProxyRequestFrame struct {
	Flags      int32
	ExtConnID  int64
	RemoteIP   [20]byte
	OurIP      [20]byte
	ExtraBytes []byte
	Payload    []byte
}

func ParseControlFrame(frame []byte) (ControlFrame, error) {
	if len(frame) < 4 {
		return ControlFrame{}, fmt.Errorf("frame too short: %d", len(frame))
	}
	op := binary.LittleEndian.Uint32(frame[:4])
	switch op {
	case RPCProxyAns:
		if len(frame) < 16 {
			return ControlFrame{}, fmt.Errorf("proxy answer frame too short: %d", len(frame))
		}
		return ControlFrame{
			Kind:      ControlFrameProxyAns,
			Op:        op,
			Flags:     int32(binary.LittleEndian.Uint32(frame[4:8])),
			OutConnID: int64(binary.LittleEndian.Uint64(frame[8:16])),
			Payload:   append([]byte(nil), frame[16:]...),
		}, nil
	case RPCSimpleAck:
		if len(frame) != 16 {
			return ControlFrame{}, fmt.Errorf("simple ack frame length mismatch: %d", len(frame))
		}
		return ControlFrame{
			Kind:      ControlFrameSimpleAck,
			Op:        op,
			OutConnID: int64(binary.LittleEndian.Uint64(frame[4:12])),
			Confirm:   int32(binary.LittleEndian.Uint32(frame[12:16])),
		}, nil
	case RPCCloseExt:
		if len(frame) != 12 {
			return ControlFrame{}, fmt.Errorf("close ext frame length mismatch: %d", len(frame))
		}
		return ControlFrame{
			Kind:      ControlFrameCloseExt,
			Op:        op,
			OutConnID: int64(binary.LittleEndian.Uint64(frame[4:12])),
		}, nil
	default:
		return ControlFrame{}, fmt.Errorf("unexpected control op: 0x%08x", op)
	}
}

func BuildProxyAns(flags int32, outConnID int64, payload []byte) []byte {
	buf := make([]byte, 16+len(payload))
	binary.LittleEndian.PutUint32(buf[0:4], RPCProxyAns)
	binary.LittleEndian.PutUint32(buf[4:8], uint32(flags))
	binary.LittleEndian.PutUint64(buf[8:16], uint64(outConnID))
	copy(buf[16:], payload)
	return buf
}

func BuildSimpleAck(outConnID int64, confirm int32) []byte {
	buf := make([]byte, 16)
	binary.LittleEndian.PutUint32(buf[0:4], RPCSimpleAck)
	binary.LittleEndian.PutUint64(buf[4:12], uint64(outConnID))
	binary.LittleEndian.PutUint32(buf[12:16], uint32(confirm))
	return buf
}

func BuildCloseExt(outConnID int64) []byte {
	buf := make([]byte, 12)
	binary.LittleEndian.PutUint32(buf[0:4], RPCCloseExt)
	binary.LittleEndian.PutUint64(buf[4:12], uint64(outConnID))
	return buf
}

func BuildProxyReq(req ProxyRequestFrame) []byte {
	hasExtra := (req.Flags & 12) != 0
	size := 4 + 4 + 8 + 20 + 20 + len(req.Payload)
	if hasExtra {
		size += 4 + len(req.ExtraBytes)
	}
	buf := make([]byte, size)
	pos := 0
	binary.LittleEndian.PutUint32(buf[pos:pos+4], RPCProxyReq)
	pos += 4
	binary.LittleEndian.PutUint32(buf[pos:pos+4], uint32(req.Flags))
	pos += 4
	binary.LittleEndian.PutUint64(buf[pos:pos+8], uint64(req.ExtConnID))
	pos += 8
	copy(buf[pos:pos+20], req.RemoteIP[:])
	pos += 20
	copy(buf[pos:pos+20], req.OurIP[:])
	pos += 20
	if hasExtra {
		binary.LittleEndian.PutUint32(buf[pos:pos+4], uint32(len(req.ExtraBytes)))
		pos += 4
		copy(buf[pos:pos+len(req.ExtraBytes)], req.ExtraBytes)
		pos += len(req.ExtraBytes)
	}
	copy(buf[pos:], req.Payload)
	return buf
}

func ParseProxyReq(frame []byte) (ProxyRequestFrame, error) {
	if len(frame) < 56 {
		return ProxyRequestFrame{}, fmt.Errorf("proxy req frame too short: %d", len(frame))
	}
	op := binary.LittleEndian.Uint32(frame[0:4])
	if op != RPCProxyReq {
		return ProxyRequestFrame{}, fmt.Errorf("unexpected proxy req op: 0x%08x", op)
	}

	var req ProxyRequestFrame
	req.Flags = int32(binary.LittleEndian.Uint32(frame[4:8]))
	req.ExtConnID = int64(binary.LittleEndian.Uint64(frame[8:16]))
	copy(req.RemoteIP[:], frame[16:36])
	copy(req.OurIP[:], frame[36:56])

	pos := 56
	if (req.Flags & 12) != 0 {
		if len(frame) < pos+4 {
			return ProxyRequestFrame{}, fmt.Errorf("proxy req missing extra size")
		}
		extraLen := int(binary.LittleEndian.Uint32(frame[pos : pos+4]))
		pos += 4
		if extraLen < 0 || len(frame) < pos+extraLen {
			return ProxyRequestFrame{}, fmt.Errorf("proxy req bad extra size: %d", extraLen)
		}
		req.ExtraBytes = append([]byte(nil), frame[pos:pos+extraLen]...)
		pos += extraLen
	}
	req.Payload = append([]byte(nil), frame[pos:]...)
	return req, nil
}

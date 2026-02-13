package protocol

const (
	CodeReqPQ             uint32 = 0x60469778
	CodeReqPQMulti        uint32 = 0xbe7e8ef1
	CodeReqDHParams       uint32 = 0xd712e4be
	CodeSetClientDHParams uint32 = 0xf5045f1f
	RPCProxyReq           uint32 = 0x36cef1ee
	RPCProxyAns           uint32 = 0x4403da0d
	RPCCloseConn          uint32 = 0x1fcf425d
	RPCCloseExt           uint32 = 0x5eb634a2
	RPCSimpleAck          uint32 = 0x3bac409b
	RPCPing               uint32 = 0x5730a2df
	RPCPong               uint32 = 0x8430eaa7

	encryptedMessageMinSize = 56
)

func IsDHHandshakeFunction(op uint32) bool {
	switch op {
	case CodeReqPQ, CodeReqPQMulti, CodeReqDHParams, CodeSetClientDHParams:
		return true
	default:
		return false
	}
}

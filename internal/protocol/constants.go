package protocol

// RPC-константы из common/rpc-const.h

const (
	// Базовые RPC-операции
	TLStat = 0x9d56e6b2

	RPCInvokeReq       = 0x2374df3d
	RPCInvokeKPHPReq   = 0x99a37fda
	RPCReqRunning      = 0x346d5efa
	RPCReqError        = 0x7ae432f5
	RPCReqResult       = 0x63aeda4e
	RPCReady           = 0x6a34cac7
	RPCStopReady       = 0x59d86654
	RPCSendSessionMsg  = 0x1ed5a3cc
	RPCResponseIndirect = 0x2194f56e
	RPCPing            = 0x5730a2df
	RPCPong            = 0x8430eaa7

	RPCDestActor      = 0x7568aabd
	RPCDestActorFlags = 0xf0a5acf7
	RPCDestFlags      = 0xe352035e
	RPCReqResultFlags = 0x8cc84ce1

	// Proxy-специфические RPC-операции (из mtproto/mtproto-common.h)
	RPCProxyReq  = 0x36cef1ee
	RPCProxyAns  = 0x4403da0d
	RPCCloseConn = 0x1fcf425d
	RPCCloseExt  = 0x5eb634a2
	RPCSimpleAck = 0x3bac409b

	// TL-тип proxy tag
	TLProxyTag = 0xdb1e26ae

	// TL-типы базовых значений
	TLBoolTrue  = 0x997275b5
	TLBoolFalse = 0xbc799737
	TLBoolStat  = 0x92cbcbfa
	TLTrue      = 0x3fedd339

	TLInt    = 0xa8509bda
	TLLong   = 0x22076cba
	TLDouble = 0x2210c154
	TLString = 0xb5286e24

	TLMaybeTrue  = 0x3f9c8ef8
	TLMaybeFalse = 0x27930a7b

	TLVector      = 0x1cb5c415
	TLVectorTotal = 0x10133f47
	TLTuple       = 0x9770768a

	TLDictionary = 0x1f4c618f

	TLEngineNop = 0x166bb7c6

	MaxTLStringLength = 0xffffff
	TLErrorRetry      = 503

	// Коды ошибок: синтаксис (-1000..-1999)
	TLErrorSyntax           = -1000
	TLErrorExtraData        = -1001
	TLErrorHeader           = -1002
	TLErrorWrongQueryID     = -1003
	TLErrorNotEnoughData    = -1004

	// Коды ошибок: нельзя запустить запрос (-2000..-2999)
	TLErrorUnknownFunctionID    = -2000
	TLErrorProxyNoTarget        = -2001
	TLErrorWrongActorID         = -2002
	TLErrorTooLongString        = -2003
	TLErrorValueNotInRange      = -2004
	TLErrorQueryIncorrect       = -2005
	TLErrorBadValue             = -2006
	TLErrorBinlogDisabled       = -2007
	TLErrorFeatureDisabled      = -2008
	TLErrorQueryIsEmpty         = -2009
	TLErrorInvalidConnectionID  = -2010
	TLErrorWrongSplit           = -2011
	TLErrorTooBigOffset         = -2012

	// Коды ошибок: обработка запроса (-3000..-3999)
	TLErrorQueryTimeout           = -3000
	TLErrorProxyInvalidResponse   = -3001
	TLErrorNoConnections          = -3002
	TLErrorInternal               = -3003
	TLErrorAIOFail                = -3004
	TLErrorAIOTimeout             = -3005
	TLErrorBinlogWaitTimeout      = -3006
	TLErrorAIOMaxRetryExceeded    = -3007
	TLErrorTTL                    = -3008
	TLErrorBadMetafile            = -3009
	TLErrorNotReady               = -3010

	TLErrorStorageCacheMiss           = -3500
	TLErrorStorageCacheNoMtprotoConn  = -3501

	// Разные ошибки (-4000..-4999)
	TLErrorUnknown = -4000

	// Флаги RPC_PROXY_REQ
	FlagExtNode  = 0x1000 // соединение через ext (TCP RPC ext server)
	FlagProxyTag = 0x8    // есть proxy_tag в extra bytes
	FlagDH       = 0x2    // DH-рукопожатие (нешифрованное)
	FlagHTTP     = 0x4    // HTTP extra bytes

	// DH-коды из mtproto/mtproto-common.h
	CodeReqPQ           = 0x60469778
	CodeReqPQMulti      = 0xbe7e8ef1
	CodeReqDHParams     = 0xd712e4be
	CodeSetClientDH     = 0xf5045f1f

	// Размеры
	EncryptedMessageMinSize = 56 // auth_key_id(8) + msg_key(16) + server_salt(8) + session_id(8) + msg_id(8) + seq_no(4) + msg_len(4)

	// Размеры proxy extra bytes при наличии proxy_tag:
	// [4B TL_PROXY_TAG][1B len=16][16B tag][3B padding] = 24 bytes
	ProxyTagExtraBytes = 24

	MaxMessageInts      = 1048576
	MaxProxyExtraBytes  = 16384

	// Флаги RPC_PROXY_ANS
	RPCProxyAnsSmallError   = 0x10
	RPCProxyAnsFlushNow     = 0x8
)

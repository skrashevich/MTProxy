package protocol

type SessionState int

const (
	SessionStateInit SessionState = iota
	SessionStateHandshake
	SessionStateEncrypted
)

type Session struct {
	state SessionState
}

func NewSession() *Session {
	return &Session{state: SessionStateInit}
}

func (s *Session) State() SessionState {
	return s.state
}

func (s *Session) AcceptPacket(frame []byte) (PacketInfo, error) {
	info, err := ParseMTProtoPacket(frame)
	if err != nil {
		return PacketInfo{}, err
	}
	s.AcceptInfo(info)
	return info, nil
}

func (s *Session) AcceptInfo(info PacketInfo) {
	switch info.Kind {
	case PacketKindEncrypted:
		s.state = SessionStateEncrypted
	case PacketKindDHHandshake:
		if s.state != SessionStateEncrypted {
			s.state = SessionStateHandshake
		}
	}
}

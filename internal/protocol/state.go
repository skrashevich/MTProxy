package protocol

import "sync"

// ConnPhase описывает фазу жизненного цикла соединения.
type ConnPhase int

const (
	PhaseInit      ConnPhase = iota // начальное состояние
	PhaseDH                         // DH-рукопожатие (нешифрованные пакеты)
	PhaseEncrypted                  // зашифрованный обмен
	PhaseClosed                     // соединение закрыто
)

// ConnState хранит состояние одного клиентского соединения.
type ConnState struct {
	mu sync.Mutex

	Phase     ConnPhase
	ConnID    int64  // ext_conn_id, уникальный идентификатор соединения
	AuthKeyID int64  // auth_key_id клиента (0 пока не известен)
	SecretID  int    // индекс секрета (-1 если не определён)

	// Счётчики пакетов
	PacketsIn  uint64
	PacketsOut uint64

	// Флаг: соединение помечено как dropped (RPC_F_DROPPED)
	Dropped bool
}

// NewConnState создаёт новое состояние соединения.
func NewConnState(connID int64, secretID int) *ConnState {
	return &ConnState{
		Phase:    PhaseInit,
		ConnID:   connID,
		SecretID: secretID,
	}
}

// SetPhase атомарно устанавливает фазу соединения.
func (s *ConnState) SetPhase(phase ConnPhase) {
	s.mu.Lock()
	s.Phase = phase
	s.mu.Unlock()
}

// GetPhase возвращает текущую фазу соединения.
func (s *ConnState) GetPhase() ConnPhase {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Phase
}

// SetAuthKeyID обновляет auth_key_id и переводит соединение в фазу Encrypted.
func (s *ConnState) SetAuthKeyID(id int64) {
	s.mu.Lock()
	s.AuthKeyID = id
	if id != 0 {
		s.Phase = PhaseEncrypted
	}
	s.mu.Unlock()
}

// MarkDropped помечает соединение как dropped.
func (s *ConnState) MarkDropped() {
	s.mu.Lock()
	s.Dropped = true
	s.mu.Unlock()
}

// IsDropped возвращает true если соединение помечено как dropped.
func (s *ConnState) IsDropped() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Dropped
}

// IncrPacketsIn атомарно увеличивает счётчик входящих пакетов.
func (s *ConnState) IncrPacketsIn() {
	s.mu.Lock()
	s.PacketsIn++
	s.mu.Unlock()
}

// IncrPacketsOut атомарно увеличивает счётчик исходящих пакетов.
func (s *ConnState) IncrPacketsOut() {
	s.mu.Lock()
	s.PacketsOut++
	s.mu.Unlock()
}

// ConnStateMap — потокобезопасная таблица состояний соединений.
type ConnStateMap struct {
	mu    sync.RWMutex
	conns map[int64]*ConnState
}

// NewConnStateMap создаёт новую пустую таблицу.
func NewConnStateMap() *ConnStateMap {
	return &ConnStateMap{
		conns: make(map[int64]*ConnState),
	}
}

// Add добавляет состояние соединения.
func (m *ConnStateMap) Add(s *ConnState) {
	m.mu.Lock()
	m.conns[s.ConnID] = s
	m.mu.Unlock()
}

// Get возвращает состояние соединения по connID.
func (m *ConnStateMap) Get(connID int64) (*ConnState, bool) {
	m.mu.RLock()
	s, ok := m.conns[connID]
	m.mu.RUnlock()
	return s, ok
}

// Remove удаляет состояние соединения.
func (m *ConnStateMap) Remove(connID int64) {
	m.mu.Lock()
	delete(m.conns, connID)
	m.mu.Unlock()
}

// Len возвращает количество активных соединений.
func (m *ConnStateMap) Len() int {
	m.mu.RLock()
	n := len(m.conns)
	m.mu.RUnlock()
	return n
}

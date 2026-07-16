package session

import (
	"context"
	"sync"
	"time"
)

// MemoryStore keeps sessions in a plain map. Fine for single-instance CPs;
// restarts log everyone out. Swap for a DB-backed store when running HA.
type MemoryStore struct {
	mu       sync.Mutex
	sessions map[string]Session
	// lastGC caps how often the sweep runs; we lazy-clean on Get() rather
	// than spinning a goroutine — no leaks on shutdown.
	lastGC time.Time
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{sessions: map[string]Session{}}
}

func (m *MemoryStore) Create(_ context.Context, s Session) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions[s.ID] = s
	return nil
}

func (m *MemoryStore) Get(_ context.Context, id string) (Session, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.gcLocked()
	s, ok := m.sessions[id]
	if !ok {
		return Session{}, false
	}
	if time.Now().After(s.ExpiresAt) {
		delete(m.sessions, id)
		return Session{}, false
	}
	return s, true
}

func (m *MemoryStore) Delete(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, id)
	return nil
}

// gcLocked drops expired sessions. Cheap enough to run on every Get once per
// minute; keeps the map from growing unbounded for churny sessions.
func (m *MemoryStore) gcLocked() {
	now := time.Now()
	if now.Sub(m.lastGC) < time.Minute {
		return
	}
	m.lastGC = now
	for id, s := range m.sessions {
		if now.After(s.ExpiresAt) {
			delete(m.sessions, id)
		}
	}
}

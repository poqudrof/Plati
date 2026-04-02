package services

import "sync"

// CreationLogManager buffers creation log lines per instance and fans them out
// to any connected WebSocket subscribers in real time.
type CreationLogManager struct {
	mu      sync.Mutex
	entries map[int64]*creationEntry
}

type creationEntry struct {
	lines []string
	subs  []chan string
	done  bool
}

func NewCreationLogManager() *CreationLogManager {
	return &CreationLogManager{entries: make(map[int64]*creationEntry)}
}

// Log appends a line and delivers it to all active subscribers.
func (m *CreationLogManager) Log(id int64, line string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e := m.entry(id)
	e.lines = append(e.lines, line)
	for _, ch := range e.subs {
		select {
		case ch <- line:
		default: // slow subscriber: drop rather than block
		}
	}
}

// Complete marks the entry done, closes all subscriber channels, and clears the
// subscriber list. Subsequent Log calls have no effect on closed channels.
func (m *CreationLogManager) Complete(id int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e := m.entry(id)
	if e.done {
		return
	}
	e.done = true
	for _, ch := range e.subs {
		close(ch)
	}
	e.subs = nil
}

// Subscribe returns all lines logged so far and a channel that receives future
// lines. Returns ch=nil if creation is already complete.
func (m *CreationLogManager) Subscribe(id int64) (existing []string, ch chan string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e := m.entry(id)
	existing = make([]string, len(e.lines))
	copy(existing, e.lines)
	if e.done {
		return existing, nil
	}
	ch = make(chan string, 128)
	e.subs = append(e.subs, ch)
	return existing, ch
}

// Unsubscribe removes a channel from the subscriber list (called on WebSocket close).
func (m *CreationLogManager) Unsubscribe(id int64, ch chan string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.entries[id]
	if !ok {
		return
	}
	for i, sub := range e.subs {
		if sub == ch {
			e.subs = append(e.subs[:i], e.subs[i+1:]...)
			return
		}
	}
}

func (m *CreationLogManager) entry(id int64) *creationEntry {
	if e, ok := m.entries[id]; ok {
		return e
	}
	e := &creationEntry{}
	m.entries[id] = e
	return e
}

package incus

import (
	"fmt"
	"sync"
)

// Pool manages multiple Incus server connections.
type Pool struct {
	mu      sync.RWMutex
	clients map[string]IncusClient
}

func NewPool() *Pool {
	return &Pool{
		clients: make(map[string]IncusClient),
	}
}

func (p *Pool) AddServer(name, endpoint, certPath, keyPath string) error {
	client, err := NewClient(name, endpoint, certPath, keyPath)
	if err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.clients[name] = client
	return nil
}

// SetClient injects a pre-built client (used in tests to inject mocks).
func (p *Pool) SetClient(name string, client IncusClient) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.clients[name] = client
}

func (p *Pool) GetClient(name string) (IncusClient, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	client, ok := p.clients[name]
	if !ok {
		return nil, fmt.Errorf("server %q not found in pool", name)
	}
	return client, nil
}

func (p *Pool) ListServers() []string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	names := make([]string, 0, len(p.clients))
	for name := range p.clients {
		names = append(names, name)
	}
	return names
}

package environment

import (
	"fmt"
	"sync"
)

// Pool manages a fixed set of host ports for stream environments.
// Thread-safe for concurrent allocation and release.
type Pool struct {
	mu       sync.Mutex
	basePort int
	slots    []bool // true = in use
}

// NewPool creates a port pool starting at basePort with the given size.
func NewPool(basePort, size int) *Pool {
	return &Pool{
		basePort: basePort,
		slots:    make([]bool, size),
	}
}

// Allocate returns the next available port from the pool.
// Returns an error if all ports are in use.
func (p *Pool) Allocate() (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for i, used := range p.slots {
		if !used {
			p.slots[i] = true
			return p.basePort + i, nil
		}
	}
	return 0, fmt.Errorf("no available ports (pool exhausted, %d slots)", len(p.slots))
}

// Release returns a port to the pool. No-op if the port is outside the pool range.
func (p *Pool) Release(port int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	idx := port - p.basePort
	if idx >= 0 && idx < len(p.slots) {
		p.slots[idx] = false
	}
}

// Size returns the total number of slots in the pool.
func (p *Pool) Size() int {
	return len(p.slots)
}

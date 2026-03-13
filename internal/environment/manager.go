package environment

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
)

const (
	defaultBasePort = 4010
	defaultPoolSize = 8
)

// Manager orchestrates environment provisioning and teardown across streams.
type Manager struct {
	mu     sync.Mutex
	config *Config
	pool   *Pool
	envs   map[string]*Environment // streamID → Environment
}

// NewManager creates an environment manager. If cfg is nil, the manager is
// inert — Ensure() returns nil without error, and teardown is a no-op.
func NewManager(cfg *Config) *Manager {
	return &Manager{
		config: cfg,
		pool:   NewPool(defaultBasePort, defaultPoolSize),
		envs:   make(map[string]*Environment),
	}
}

// Enabled returns true if the manager has a valid configuration.
func (m *Manager) Enabled() bool {
	return m.config != nil
}

// Ensure idempotently provisions an environment for the given stream.
// Returns the existing environment if already Ready, provisions a new one
// if none exists, or returns nil if environments are not configured.
func (m *Manager) Ensure(ctx context.Context, streamID, workdir string) (*Environment, error) {
	if m.config == nil {
		return nil, nil
	}

	m.mu.Lock()
	if env, ok := m.envs[streamID]; ok {
		m.mu.Unlock()
		if env.Status == StatusReady {
			return env, nil
		}
		if env.Status == StatusFailed {
			return nil, fmt.Errorf("environment previously failed: %s", env.Error)
		}
		return env, nil
	}

	// Allocate port while holding the lock.
	port, err := m.pool.Allocate()
	if err != nil {
		m.mu.Unlock()
		return nil, fmt.Errorf("allocate port: %w", err)
	}

	projectName := "streams-" + streamID
	env := &Environment{
		ProjectName: projectName,
		HostPort:    port,
		Status:      StatusProvisioning,
	}
	m.envs[streamID] = env
	m.mu.Unlock()

	// Provision outside the lock.
	if err := m.provision(ctx, env, workdir); err != nil {
		m.mu.Lock()
		env.Status = StatusFailed
		env.Error = err.Error()
		m.mu.Unlock()
		slog.Error("environment provision failed", "stream", streamID, "err", err)
		return nil, err
	}

	m.mu.Lock()
	env.Status = StatusReady
	m.mu.Unlock()

	slog.Info("environment ready", "stream", streamID, "port", port)
	return env, nil
}

func (m *Manager) provision(ctx context.Context, env *Environment, workdir string) error {
	if err := Up(ctx, workdir, m.config, env.ProjectName, env.HostPort); err != nil {
		return fmt.Errorf("compose up: %w", err)
	}

	if err := HealthCheck(ctx, env.HostPort, m.config.HealthCheck, m.config.HealthTimeout); err != nil {
		// Tear down on health check failure to free resources.
		Down(context.Background(), env.ProjectName)
		return fmt.Errorf("health check: %w", err)
	}

	if err := Exec(ctx, env.ProjectName, m.config.Service, m.config.Setup); err != nil {
		Down(context.Background(), env.ProjectName)
		return fmt.Errorf("setup command: %w", err)
	}

	return nil
}

// Teardown tears down the environment for a stream and releases its port.
func (m *Manager) Teardown(ctx context.Context, streamID string) error {
	m.mu.Lock()
	env, ok := m.envs[streamID]
	if !ok {
		m.mu.Unlock()
		return nil
	}
	delete(m.envs, streamID)
	m.mu.Unlock()

	if env.Status != StatusDown {
		if err := Down(ctx, env.ProjectName); err != nil {
			slog.Warn("environment teardown failed", "stream", streamID, "err", err)
		}
	}

	m.pool.Release(env.HostPort)
	slog.Info("environment torn down", "stream", streamID, "port", env.HostPort)
	return nil
}

// TeardownAll tears down all active environments. Called on application exit.
func (m *Manager) TeardownAll(ctx context.Context) {
	m.mu.Lock()
	ids := make([]string, 0, len(m.envs))
	for id := range m.envs {
		ids = append(ids, id)
	}
	m.mu.Unlock()

	for _, id := range ids {
		m.Teardown(ctx, id)
	}
}

// Get returns the environment for a stream, or nil if none exists.
func (m *Manager) Get(streamID string) *Environment {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.envs[streamID]
}

// PortForStream returns the host port for a stream's environment, or 0 if none.
func (m *Manager) PortForStream(streamID string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	if env, ok := m.envs[streamID]; ok && env.Status == StatusReady {
		return env.HostPort
	}
	return 0
}

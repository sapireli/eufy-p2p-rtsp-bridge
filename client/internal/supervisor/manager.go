package supervisor

import (
	"context"
	"sync"

	"eufy-wall/internal/config"
	"eufy-wall/internal/pipeline"
)

// Manager keeps a SET of pipelines alive, each restarting on its own.
//
// Phase 1 ran the whole wall as one process, which meant one flaky camera restarted every tile. Where
// the sink allows it (DRM planes), each tile is now its own process, so a camera dropping out costs that
// tile alone.
//
// Update swaps the set. A plan whose arguments have not changed KEEPS RUNNING — restarting a tile that
// is already showing the right thing would be a visible glitch for no reason — so repointing one tile
// touches one process and leaves the rest of the wall alone.
type Manager struct {
	bin     string
	restart config.Restart
	log     func(name, line string)

	mu      sync.Mutex
	running map[string]*proc
	done    chan struct{}
}

type proc struct {
	args   []string
	cancel context.CancelFunc
	exited chan struct{}
}

func NewManager(bin string, r config.Restart, log func(name, line string)) *Manager {
	return &Manager{bin: bin, restart: r, log: log, running: map[string]*proc{}, done: make(chan struct{})}
}

// Run starts `plans` and blocks until ctx is cancelled, then stops everything it started.
func (m *Manager) Run(ctx context.Context, plans []pipeline.Plan) error {
	m.Update(ctx, plans)
	<-ctx.Done()
	m.stopAll()
	return nil
}

// Update reconciles the running set against `plans`: start what is new, stop what is gone, and leave
// anything whose args are identical exactly as it is.
func (m *Manager) Update(ctx context.Context, plans []pipeline.Plan) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if ctx.Err() != nil {
		return
	}

	wanted := make(map[string]pipeline.Plan, len(plans))
	for _, p := range plans {
		wanted[p.Name] = p
	}

	for name, p := range m.running {
		want, keep := wanted[name]
		if keep && sameArgs(p.args, want.Args) {
			continue // already showing the right thing
		}
		m.log(name, "stopping")
		p.cancel()
		<-p.exited
		delete(m.running, name)
	}

	for name, plan := range wanted {
		if _, already := m.running[name]; already {
			continue
		}
		pctx, cancel := context.WithCancel(ctx)
		p := &proc{args: plan.Args, cancel: cancel, exited: make(chan struct{})}
		m.running[name] = p
		go func(n string, args []string) {
			defer close(p.exited)
			_ = Run(pctx, m.bin, args, m.restart, func(line string) { m.log(n, line) })
		}(name, plan.Args)
	}
}

// Names lists what is running, for logs and tests.
func (m *Manager) Names() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, 0, len(m.running))
	for n := range m.running {
		out = append(out, n)
	}
	return out
}

func (m *Manager) stopAll() {
	m.mu.Lock()
	procs := make([]*proc, 0, len(m.running))
	for name, p := range m.running {
		procs = append(procs, p)
		delete(m.running, name)
	}
	m.mu.Unlock()
	for _, p := range procs {
		p.cancel()
		<-p.exited
	}
}

func sameArgs(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

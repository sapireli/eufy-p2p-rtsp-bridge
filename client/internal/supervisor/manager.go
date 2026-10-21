package supervisor

import (
	"context"
	"os"
	"sort"
	"sync"
	"sync/atomic"

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
	files   []*os.File

	reconcile sync.Mutex
	mu        sync.Mutex
	running   map[string]*proc
	done      chan struct{}
}

type proc struct {
	args     []string
	fallback []string
	software []string
	cancel   context.CancelFunc
	exited   chan struct{}
	ctx      context.Context
	demote   chan chan struct{}
	hardware atomic.Bool
	active   atomic.Bool
	pinned   bool
	priority int
	decoder  string
}

func NewManager(bin string, r config.Restart, log func(name, line string), files ...*os.File) *Manager {
	return &Manager{bin: bin, restart: r, log: log, files: files, running: map[string]*proc{}, done: make(chan struct{})}
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
	m.reconcile.Lock()
	defer m.reconcile.Unlock()
	m.mu.Lock()
	if ctx.Err() != nil {
		m.mu.Unlock()
		return
	}

	wanted := make(map[string]pipeline.Plan, len(plans))
	for _, p := range plans {
		wanted[p.Name] = p
	}

	var stopping []*proc
	for name, p := range m.running {
		want, keep := wanted[name]
		if keep && sameArgs(p.args, want.Args) && sameArgs(p.fallback, want.FallbackArgs) && sameArgs(p.software, want.SoftwareArgs) && p.pinned == want.HardwarePinned && p.priority == want.HardwarePriority && p.decoder == want.HardwareDecoder {
			continue // already showing the right thing
		}
		m.log(name, "stopping")
		delete(m.running, name)
		stopping = append(stopping, p)
	}
	m.mu.Unlock()
	for _, p := range stopping {
		p.cancel()
	}
	for _, p := range stopping {
		<-p.exited
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if ctx.Err() != nil {
		return
	}
	// Deterministic startup gives the preferred cameras the first hardware attempt.
	names := make([]string, 0, len(wanted))
	for name := range wanted {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		a, b := wanted[names[i]], wanted[names[j]]
		if a.HardwarePinned != b.HardwarePinned {
			return a.HardwarePinned
		}
		if a.HardwarePriority != b.HardwarePriority {
			return a.HardwarePriority > b.HardwarePriority
		}
		return names[i] < names[j]
	})
	for _, name := range names {
		plan := wanted[name]
		if _, already := m.running[name]; already {
			continue
		}
		if len(plan.FallbackArgs) > 0 {
			m.log(name, "selecting native hardware rendition; compatible fallback available")
		}
		pctx, cancel := context.WithCancel(ctx)
		p := &proc{args: plan.Args, fallback: plan.FallbackArgs, software: plan.SoftwareArgs, cancel: cancel, exited: make(chan struct{}), ctx: pctx, demote: make(chan chan struct{}, 1), pinned: plan.HardwarePinned, priority: plan.HardwarePriority, decoder: plan.HardwareDecoder}
		p.hardware.Store(plan.HardwareDecoder != "")
		m.running[name] = p
		go func(n string, args, fallback, software []string) {
			defer close(p.exited)
			control := &decoderControl{demote: p.demote, reclaim: func() bool { return m.reclaim(n, p) }, onSoftware: func() { p.hardware.Store(false) }, onRunning: func(active bool) { p.active.Store(active) }}
			_ = runWithDecoderControl(pctx, m.bin, args, fallback, software, m.restart, func(line string) { m.log(n, line) }, control, m.files...)
		}(name, plan.Args, plan.FallbackArgs, plan.SoftwareArgs)
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

// Stop waits for every child to release its shared DRM descriptor before display resources are freed.
// The caller cancels the parent context first so Update cannot start replacement children.
func (m *Manager) Stop() { m.stopAll() }

func (m *Manager) stopAll() {
	m.reconcile.Lock()
	defer m.reconcile.Unlock()
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

// reclaim releases one same-pool decoder before allowing the requester to retry.
// Selection is driven by an actual allocation failure, never an assumed capacity.
func (m *Manager) reclaim(name string, requester *proc) bool {
	m.mu.Lock()
	if m.running[name] != requester || requester.ctx.Err() != nil || requester.decoder == "" {
		m.mu.Unlock()
		return false
	}
	var victim *proc
	var victimName string
	for n, candidate := range m.running {
		if candidate == requester || candidate.pinned || candidate.decoder != requester.decoder || len(candidate.software) == 0 || !candidate.hardware.Load() || !candidate.active.Load() || candidate.ctx.Err() != nil {
			continue
		}
		if !requester.pinned && requester.priority <= candidate.priority {
			continue
		}
		if victim == nil || candidate.priority < victim.priority || (candidate.priority == victim.priority && n < victimName) {
			victim, victimName = candidate, n
		}
	}
	if victim == nil || !victim.hardware.CompareAndSwap(true, false) {
		m.mu.Unlock()
		return false
	}
	m.log(victimName, "yielding hardware decoder to higher-priority camera "+name)
	ack := make(chan struct{})
	victim.demote <- ack
	m.mu.Unlock()
	select {
	case <-ack:
		return requester.ctx.Err() == nil
	case <-victim.exited:
		return requester.ctx.Err() == nil
	case <-requester.ctx.Done():
		return false
	}
}

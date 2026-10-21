// Package supervisor keeps one gst-launch process alive: restart on exit with exponential backoff,
// forward its output to the log, and shut it down cleanly on cancel.
package supervisor

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"eufy-wall/internal/config"
)

type backoff struct {
	r   config.Restart
	cur time.Duration
}

func newBackoff(r config.Restart) *backoff { return &backoff{r: r} }

// next returns the delay before the next start, given how long the previous run lasted.
func (b *backoff) next(ran time.Duration) time.Duration {
	min := time.Duration(b.r.MinSeconds) * time.Second
	max := time.Duration(b.r.MaxSeconds) * time.Second
	if ran >= time.Duration(b.r.StableSeconds)*time.Second || b.cur == 0 {
		b.cur = min
	} else {
		b.cur *= 2
		if b.cur > max {
			b.cur = max
		}
	}
	return b.cur
}

func pump(r io.Reader, log func(string)) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		log(sc.Text())
	}
}

// Run blocks until ctx is cancelled. It never returns a process error; failures are logged and retried.
func Run(ctx context.Context, bin string, args []string, r config.Restart, log func(string), files ...*os.File) error {
	return RunWithFallback(ctx, bin, args, nil, r, log, files...)
}

// RunWithFallback switches once to a compatible plan after a native process
// fails. Further retries stay on that fallback until this managed plan changes.
func RunWithFallback(ctx context.Context, bin string, args, fallback []string, r config.Restart, log func(string), files ...*os.File) error {
	return runWithDecoderFallback(ctx, bin, args, fallback, nil, r, log, files...)
}

func runWithDecoderFallback(ctx context.Context, bin string, args, fallback, software []string, r config.Restart, log func(string), files ...*os.File) error {
	return runWithDecoderControl(ctx, bin, args, fallback, software, r, log, nil, files...)
}

// decoderControl lets the manager relinquish one lower-ranked decoder without
// stopping other tiles. A demotion is acknowledged only after process teardown.
type decoderControl struct {
	demote     <-chan chan struct{}
	reclaim    func() bool
	onSoftware func()
	onRunning  func(bool)
}

func runWithDecoderControl(ctx context.Context, bin string, args, fallback, software []string, r config.Restart, log func(string), control *decoderControl, files ...*os.File) error {
	b := newBackoff(r)
	reclaimed := false
	var demote <-chan chan struct{}
	if control != nil {
		demote = control.demote
		if control.onRunning != nil {
			defer control.onRunning(false)
		}
	}
	selectSoftware := func() {
		if len(software) == 0 {
			return
		}
		args, fallback, software = software, nil, nil
		if control != nil && control.onSoftware != nil {
			control.onSoftware()
		}
	}
	for {
		var decoderFailed, resourceFailed atomic.Bool
		var demoted chan struct{}
		if ctx.Err() != nil {
			return nil
		}
		// An allocation failure may have selected software while the manager
		// concurrently reserved this tile for demotion. Its hardware is already
		// released; acknowledge without interrupting software playback.
		if len(software) == 0 {
			select {
			case ack := <-demote:
				close(ack)
			default:
			}
		}
		cmd := exec.Command(bin, args...)
		cmd.ExtraFiles = files
		cmd.Env = append(os.Environ(), "GST_DEBUG_NO_COLOR=1", "LC_ALL=C")
		// Own the pipes: exec.Cmd.Wait must not close its StdoutPipe before
		// scanners consume the terminal decoder error from a fast exit.
		stdout, stdoutWrite, stdoutErr := os.Pipe()
		stderr, stderrWrite, stderrErr := os.Pipe()
		cmd.Stdout, cmd.Stderr = stdoutWrite, stderrWrite
		closePipes := func() {
			for _, f := range []*os.File{stdout, stdoutWrite, stderr, stderrWrite} {
				if f != nil {
					_ = f.Close()
				}
			}
		}
		start := time.Now()
		if stdoutErr != nil || stderrErr != nil {
			log(fmt.Sprintf("pipe failed: stdout=%v stderr=%v", stdoutErr, stderrErr))
			closePipes()
		} else if err := cmd.Start(); err != nil {
			log(fmt.Sprintf("start failed: %v", err))
			closePipes()
		} else {
			_ = stdoutWrite.Close()
			_ = stderrWrite.Close()
			if control != nil && control.onRunning != nil {
				control.onRunning(true)
			}
			log(fmt.Sprintf("started pid %d", cmd.Process.Pid))
			fatal := make(chan struct{}, 1)
			output := func(line string) {
				log(line)
				if hardwareResourceError(line) {
					resourceFailed.Store(true)
				}
				if hardwareInitializationError(line) {
					decoderFailed.Store(true)
				}
				// gst-launch can hang while tearing down a failed hardware pipeline.
				// A fatal bus error already ended playback; warnings are recoverable.
				if strings.HasPrefix(line, "ERROR:") {
					select {
					case fatal <- struct{}{}:
					default:
					}
				}
			}
			var pumps sync.WaitGroup
			pumps.Add(2)
			go func() { defer pumps.Done(); pump(stdout, output) }()
			go func() { defer pumps.Done(); pump(stderr, output) }()
			pumpsDone := make(chan struct{})
			go func() { pumps.Wait(); close(pumpsDone) }()
			waited := make(chan error, 1)
			go func() { waited <- cmd.Wait() }()
			select {
			case err := <-waited:
				log(fmt.Sprintf("exited after %s: %v", time.Since(start).Round(time.Second), err))
			case <-fatal:
				// Allow normal teardown first, then bound TERM-resistant drivers.
				select {
				case <-waited:
				case <-time.After(5 * time.Second):
					log("fatal pipeline error: teardown stalled; terminating process")
					_ = cmd.Process.Signal(syscall.SIGTERM)
					select {
					case <-waited:
					case <-time.After(5 * time.Second):
						log("fatal pipeline error: teardown ignored TERM; killing process")
						_ = cmd.Process.Kill()
						<-waited
					}
				}
			case demoted = <-demote:
				_ = cmd.Process.Signal(syscall.SIGTERM)
				select {
				case <-waited:
				case <-time.After(5 * time.Second):
					_ = cmd.Process.Kill()
					<-waited
				}
			case <-ctx.Done():
				_ = cmd.Process.Signal(syscall.SIGTERM)
				select {
				case <-waited:
				case <-time.After(5 * time.Second):
					_ = cmd.Process.Kill()
					<-waited
				}
				closePipes()
				<-pumpsDone
				return nil
			}
			// A descendant may retain inherited pipe writers after the parent
			// exits. Cancellation must still release this managed tile promptly.
			select {
			case <-pumpsDone:
			case <-ctx.Done():
				closePipes()
				<-pumpsDone
				return nil
			}
			closePipes()
		}
		if control != nil && control.onRunning != nil {
			control.onRunning(false)
		}
		if demoted != nil {
			log("hardware decoder yielded to higher-priority camera; selecting software for this tile")
			selectSoftware()
			close(demoted)
			continue
		}
		if !reclaimed && resourceFailed.Load() && len(software) > 0 && control != nil && control.reclaim != nil && control.reclaim() {
			reclaimed = true
			log("decoder resources released by lower-priority camera; retrying hardware")
			continue
		}
		if len(fallback) > 0 {
			log("native pipeline failed; selecting compatible fallback")
			args, fallback = fallback, nil
		} else if decoderFailed.Load() && len(software) > 0 {
			log("hardware decoder initialization failed; selecting software fallback for this tile")
			selectSoftware()
		}
		d := b.next(time.Since(start))
		log(fmt.Sprintf("restarting in %s", d))
		select {
		case <-time.After(d):
		case ack := <-demote:
			log("hardware decoder yielded to higher-priority camera; selecting software for this tile")
			selectSoftware()
			close(ack)
		case <-ctx.Done():
			return nil
		}
	}
}

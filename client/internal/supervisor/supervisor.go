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
	b := newBackoff(r)
	for {
		if ctx.Err() != nil {
			return nil
		}
		cmd := exec.Command(bin, args...)
		cmd.ExtraFiles = files
		cmd.Env = append(os.Environ(), "GST_DEBUG_NO_COLOR=1")
		stdout, stdoutErr := cmd.StdoutPipe()
		stderr, stderrErr := cmd.StderrPipe()
		start := time.Now()
		if stdoutErr != nil || stderrErr != nil {
			log(fmt.Sprintf("pipe failed: stdout=%v stderr=%v", stdoutErr, stderrErr))
		} else if err := cmd.Start(); err != nil {
			log(fmt.Sprintf("start failed: %v", err))
		} else {
			log(fmt.Sprintf("started pid %d", cmd.Process.Pid))
			fatal := make(chan struct{}, 1)
			output := func(line string) {
				log(line)
				// gst-launch can hang while tearing down a failed hardware pipeline.
				// A fatal bus error already ended playback; warnings are recoverable.
				if strings.HasPrefix(line, "ERROR:") {
					select {
					case fatal <- struct{}{}:
					default:
					}
				}
			}
			go pump(stdout, output)
			go pump(stderr, output)
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
			case <-ctx.Done():
				_ = cmd.Process.Signal(syscall.SIGTERM)
				select {
				case <-waited:
				case <-time.After(5 * time.Second):
					_ = cmd.Process.Kill()
					<-waited
				}
				return nil
			}
		}
		if len(fallback) > 0 {
			log("native pipeline failed; selecting compatible fallback")
			args, fallback = fallback, nil
		}
		d := b.next(time.Since(start))
		log(fmt.Sprintf("restarting in %s", d))
		select {
		case <-time.After(d):
		case <-ctx.Done():
			return nil
		}
	}
}

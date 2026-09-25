package main

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"golang.org/x/term"
)

func fullScreenTerminal(in io.Reader, out io.Writer) (*os.File, *os.File, bool) {
	input, inputOK := in.(*os.File)
	output, outputOK := out.(*os.File)
	if !inputOK || !outputOK || os.Getenv("EUFY_WALL_EDITOR") == "line" || os.Getenv("TERM") == "dumb" {
		return nil, nil, false
	}
	if !term.IsTerminal(int(input.Fd())) || !term.IsTerminal(int(output.Fd())) {
		return nil, nil, false
	}
	width, height, err := term.GetSize(int(output.Fd()))
	return input, output, err == nil && width >= 80 && height >= 24
}

func runLayoutScreen(editor *layoutEditor, in, out *os.File) error {
	previous, err := term.MakeRaw(int(in.Fd()))
	if err != nil {
		return fmt.Errorf("terminal raw mode: %w", err)
	}
	defer term.Restore(int(in.Fd()), previous)
	if _, err := io.WriteString(out, "\x1b[?1049h\x1b[?25l"); err != nil {
		return err
	}
	defer io.WriteString(out, "\x1b[?25h\x1b[?1049l")
	width, height, err := term.GetSize(int(out.Fd()))
	if err != nil {
		return err
	}
	s := &layoutScreen{editor: editor, step: 1, width: width, height: height, saved: bytes.Clone(editor.history[editor.at])}
	s.apply = func() error {
		if err := editor.ensureInventory(""); err != nil {
			return err
		}
		if err := editor.checkInventory(); err != nil {
			return err
		}
		var message bytes.Buffer
		if err := applyClientConfigTarget("-", bytes.NewReader(editor.history[editor.at]), &message, editor.target); err != nil {
			return err
		}
		s.status = strings.TrimSpace(message.String())
		return nil
	}
	resizes := make(chan os.Signal, 1)
	signal.Notify(resizes, syscall.SIGWINCH)
	defer signal.Stop(resizes)
	interrupts := make(chan os.Signal, 1)
	signal.Notify(interrupts, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(interrupts)
	if err := s.render(out); err != nil {
		return err
	}
	keys := make(chan screenKey, 8)
	readErrors := make(chan error, 1)
	done := make(chan struct{})
	defer close(done)
	go func() {
		reader := bufio.NewReader(in)
		for {
			key, err := readScreenKey(reader)
			if err != nil {
				select {
				case readErrors <- err:
				case <-done:
				}
				return
			}
			select {
			case keys <- key:
			case <-done:
				return
			}
		}
	}()
	for {
		select {
		case key := <-keys:
			quit, err := s.handle(key)
			if err != nil {
				s.status = "error: " + err.Error()
			}
			if quit {
				return nil
			}
		case err := <-readErrors:
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		case <-resizes:
			width, height, err = term.GetSize(int(out.Fd()))
			if err != nil {
				return err
			}
			s.width, s.height = width, height
		case signal := <-interrupts:
			return fmt.Errorf("layout editor interrupted by %s", signal)
		}
		if err := s.render(out); err != nil {
			return err
		}
	}
}

func readScreenKey(r *bufio.Reader) (screenKey, error) {
	b, err := r.ReadByte()
	if err != nil {
		return screenKey{}, err
	}
	switch b {
	case 3:
		return screenKey{name: "interrupt"}, nil
	case 7:
		return screenKey{name: "cancel"}, nil
	case 9:
		return screenKey{name: "tab"}, nil
	case 12:
		return screenKey{name: "redraw"}, nil
	case 21:
		return screenKey{name: "clear"}, nil
	case 13, 10:
		return screenKey{name: "enter"}, nil
	case 8, 127:
		return screenKey{name: "backspace"}, nil
	case 27:
		next, err := r.ReadByte()
		if err != nil {
			return screenKey{name: "escape"}, nil
		}
		if next != '[' && next != 'O' {
			_ = r.UnreadByte()
			return screenKey{name: "escape"}, nil
		}
		for i := 0; i < 16; i++ {
			ch, err := r.ReadByte()
			if err != nil {
				return screenKey{}, err
			}
			if ch >= '@' && ch <= '~' {
				switch ch {
				case 'A':
					return screenKey{name: "up"}, nil
				case 'B':
					return screenKey{name: "down"}, nil
				case 'C':
					return screenKey{name: "right"}, nil
				case 'D':
					return screenKey{name: "left"}, nil
				case 'Z':
					return screenKey{name: "backtab"}, nil
				}
				return screenKey{name: "unknown"}, nil
			}
		}
		return screenKey{name: "unknown"}, nil
	}
	if b < 32 {
		return screenKey{name: "unknown"}, nil
	}
	if b < 128 {
		return screenKey{r: rune(b)}, nil
	}
	if err := r.UnreadByte(); err != nil {
		return screenKey{}, err
	}
	ch, _, err := r.ReadRune()
	return screenKey{r: ch}, err
}

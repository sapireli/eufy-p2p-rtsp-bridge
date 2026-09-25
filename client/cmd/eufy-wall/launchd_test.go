package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLaunchdFirstApplyAndFailedFirstApply(t *testing.T) {
	plist := filepath.Join(t.TempDir(), "com.eufy.wall.plist")
	if err := os.WriteFile(plist, []byte("test"), 0600); err != nil {
		t.Fatal(err)
	}
	loaded, enabled := false, false
	var calls []string
	s := launchdWallService{
		plistPath: func() (string, error) { return plist, nil },
		run: func(args ...string) ([]byte, error) {
			calls = append(calls, strings.Join(args, " "))
			switch args[0] {
			case "enable":
				enabled = true
			case "disable":
				enabled = false
			case "print":
				if !loaded {
					return nil, errors.New("not loaded")
				}
				return []byte("state = running\n pid = 22\n"), nil
			case "bootstrap":
				if !enabled {
					t.Fatal("bootstrap before enable")
				}
				loaded = true
			case "kickstart":
				if !loaded {
					t.Fatal("kickstart before bootstrap")
				}
			case "bootout":
				loaded = false
			}
			return nil, nil
		},
	}
	if err := s.Restart(); err != nil {
		t.Fatal(err)
	}
	if !loaded || !enabled || len(calls) != 4 || !strings.HasPrefix(calls[0], "enable ") || !strings.HasPrefix(calls[2], "bootstrap ") {
		t.Fatalf("first apply calls=%v loaded=%v enabled=%v", calls, loaded, enabled)
	}
	if err := s.Healthy(); err != nil {
		t.Fatal(err)
	}
	calls = nil
	if err := s.Restart(); err != nil || len(calls) != 3 || strings.HasPrefix(calls[1], "bootstrap ") {
		t.Fatalf("loaded restart error=%v calls=%v", err, calls)
	}
	if err := s.Stop(); err != nil {
		t.Fatal(err)
	}
	if loaded || enabled {
		t.Fatalf("failed first apply left job loaded=%v enabled=%v", loaded, enabled)
	}
}

func TestLaunchdMissingPlistAndUnloadedStop(t *testing.T) {
	var calls []string
	s := launchdWallService{
		plistPath: func() (string, error) { return filepath.Join(t.TempDir(), "absent.plist"), nil },
		run: func(args ...string) ([]byte, error) {
			calls = append(calls, args[0])
			if args[0] == "print" {
				return nil, errors.New("not loaded")
			}
			return nil, nil
		},
	}
	if err := s.Restart(); err == nil || !strings.Contains(err.Error(), "not installed") {
		t.Fatalf("missing plist error=%v", err)
	}
	if err := s.Stop(); err != nil || calls[len(calls)-1] != "disable" {
		t.Fatalf("unloaded Stop error=%v calls=%v", err, calls)
	}
	if err := s.active(); err == nil {
		t.Fatal("unloaded job reported active")
	}
}

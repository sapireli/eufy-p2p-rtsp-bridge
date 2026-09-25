package main

import (
	"flag"
	"os"
	"path/filepath"
	"testing"
)

func TestMainPrintLayoutAndDryRunWithExplicitHostCaps(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wall.yaml")
	yaml := "screen: {width: 640, height: 480}\nlayout: 1\ndecoder: software\nsink: window\ntiles:\n  - url: rtsp://bridge/live\n"
	if err := os.WriteFile(path, []byte(yaml), 0600); err != nil {
		t.Fatal(err)
	}
	previousArgs, previousFlags := os.Args, flag.CommandLine
	defer func() { os.Args, flag.CommandLine = previousArgs, previousFlags }()
	for _, command := range []string{"-print-layout", "-dry-run"} {
		flag.CommandLine = flag.NewFlagSet("eufy-wall-test", flag.ContinueOnError)
		os.Args = []string{"eufy-wall", "-config", path, command}
		main()
	}
}

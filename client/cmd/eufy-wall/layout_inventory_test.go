package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"eufy-wall/internal/config"
)

func TestEditorInventoryWarnsUnknownSourcesAndBlocksApply(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/cameras" {
			t.Errorf("path=%s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`[{"sn":"KNOWN","name":"Door","codec":"h265","mode":"always"}]`))
	}))
	defer ts.Close()
	data := bytes.Replace(config.Example(), []byte("http://bridge.local:3000"), []byte(ts.URL), 1)
	path := filepath.Join(t.TempDir(), "wall.yaml")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	e, err := newLayoutEditor(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.ensureInventory(""); err != nil {
		t.Fatal(err)
	}
	c, err := e.config()
	if err != nil {
		t.Fatal(err)
	}
	if len(e.inventoryIssues(c)) != 2 {
		t.Fatalf("expected two unknown placeholders: %+v", e.inventoryIssues(c))
	}
	if err := e.checkInventory(); err == nil {
		t.Fatal("unknown camera allowed to apply")
	}
	var out bytes.Buffer
	if err := e.showInventory(&out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "KNOWN Door codec=h265") {
		t.Fatalf("inventory output: %s", out.String())
	}
	if err := e.change(func(c *config.Config) error { return editorMutation(c, []string{"camera", "garage", "KNOWN"}) }); err != nil {
		t.Fatal(err)
	}
	if err := e.change(func(c *config.Config) error { return editorMutation(c, []string{"delete", "front-door"}) }); err != nil {
		t.Fatal(err)
	}
	if err := e.checkInventory(); err != nil {
		t.Fatalf("known source rejected: %v", err)
	}
}

func TestEditorLoadsOfflineInventoryAndRejectsMalformedFile(t *testing.T) {
	e, err := newLayoutEditor("")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "inventory.json")
	if err := os.WriteFile(path, []byte(`{"schema_version":1,"cameras":[{"sn":"T8425XXXXXXXXXXX","name":"Garage"},{"sn":"T8214XXXXXXXXXXX","name":"Door"}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := e.ensureInventory(path); err != nil {
		t.Fatal(err)
	}
	if err := e.checkInventory(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"schema_version":99}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := e.ensureInventory(path); err == nil {
		t.Fatal("unsupported inventory version accepted")
	}
}

func TestEditorInventoryCommandShowsWarnings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "inventory.json")
	if err := os.WriteFile(path, []byte(`{"schema_version":1,"cameras":[{"sn":"OTHER","name":"Other"}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := editLayout("", strings.NewReader("inventory "+path+"\nquit\n"), &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "inventory warning: tile garage uses unknown camera") {
		t.Fatalf("warnings missing: %s", out.String())
	}
}

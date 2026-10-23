package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPublicBridgeIdentityPersistsAndCannotSilentlyChange(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private", "identities.json")
	if err := retainBridgeIdentity(path, "service", "A"); err != nil {
		t.Fatal(err)
	}
	ids, err := readIdentityCache(path)
	if err != nil || ids["service"] != "A" {
		t.Fatal(ids, err)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Fatal(info.Mode())
	}
	if err := retainBridgeIdentity(path, "service", "B"); err == nil {
		t.Fatal("changedidentityaccepted")
	}
	ids, _ = readIdentityCache(path)
	if ids["service"] != "A" {
		t.Fatal("oldidentityoverwritten")
	}
	os.WriteFile(path, []byte("invalid"), 0600)
	if _, err := readIdentityCache(path); err == nil {
		t.Fatal("corruptidentitycacheignored")
	}
}

func TestIdentityPinRejectsSymlinksAndSupportsExplicitReset(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.json")
	os.WriteFile(target, []byte(`{"service":"A"}`), 0600)
	link := filepath.Join(dir, "pin.json")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, err := readIdentityCache(link); err == nil {
		t.Fatal("followedidentityfilesymlink")
	}
	if clearBridgeIdentities(link) == nil {
		t.Fatal("resetsymlinkallowed")
	}
	path := filepath.Join(dir, "owned", "pins.json")
	if err := retainBridgeIdentity(path, "service", "A"); err != nil {
		t.Fatal(err)
	}
	if err := clearBridgeIdentities(path); err != nil {
		t.Fatal(err)
	}
	if err := retainBridgeIdentity(path, "service", "B"); err != nil {
		t.Fatal("explicitresetdidnotallowreplacement", err)
	}
	directoryLink := filepath.Join(dir, "linked")
	os.Symlink(filepath.Dir(target), directoryLink)
	if retainBridgeIdentity(filepath.Join(directoryLink, "new.json"), "service", "C") == nil {
		t.Fatal("followeddirectorysymlink")
	}
}

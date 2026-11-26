package main

import (
	"path/filepath"
	"testing"
)

func TestConfigSurvivesSaveAndLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "config.json")
	want := Config{X: -1200, Y: 40, HasPos: true, Topmost: false, Source: "output"}
	if err := want.Save(path); err != nil {
		t.Fatal(err)
	}
	if got := LoadConfig(path); got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestLoadConfigFallsBackToDefaultsWhenFileIsMissing(t *testing.T) {
	got := LoadConfig(filepath.Join(t.TempDir(), "nope.json"))
	want := Config{Topmost: true, Source: "input"}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestLoadConfigFallsBackToDefaultsWhenFileIsCorrupt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := writeFile(path, []byte("{not json")); err != nil {
		t.Fatal(err)
	}
	if got := LoadConfig(path); got != DefaultConfig() {
		t.Fatalf("got %+v, want defaults", got)
	}
}

func TestLoadConfigRejectsUnknownSource(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := writeFile(path, []byte(`{"source":"banana","topmost":true}`)); err != nil {
		t.Fatal(err)
	}
	if got := LoadConfig(path).Source; got != "input" {
		t.Fatalf("Source = %q, want input", got)
	}
}

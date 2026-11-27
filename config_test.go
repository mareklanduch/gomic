package main

import (
	"path/filepath"
	"testing"
)

func TestConfigSurvivesSaveAndLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "config.json")
	want := Config{
		X: -1200, Y: 40, HasPos: true, Topmost: false,
		ShowInput: true, ShowOutput: true,
		InputDevice: "{0.0.1}.{abc}", InputDeviceName: "USB Microphone",
		OutputDevice: "{0.0.0}.{def}", OutputDeviceName: "Speakers",
	}
	if err := want.Save(path); err != nil {
		t.Fatal(err)
	}
	if got := LoadConfig(path); got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestLoadConfigFallsBackToDefaultsWhenFileIsMissing(t *testing.T) {
	got := LoadConfig(filepath.Join(t.TempDir(), "nope.json"))
	want := Config{Topmost: true, ShowInput: true}
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

func TestLoadConfigShowsOnlyTheMeterAnOlderFileHadSelected(t *testing.T) {
	for source, want := range map[string][2]bool{
		"output": {false, true},
		"input":  {true, false},
		"banana": {true, false},
	} {
		path := filepath.Join(t.TempDir(), "config.json")
		if err := writeFile(path, []byte(`{"source":"`+source+`","topmost":true}`)); err != nil {
			t.Fatal(err)
		}
		c := LoadConfig(path)
		if got := [2]bool{c.ShowInput, c.ShowOutput}; got != want {
			t.Errorf("source %q: show input/output = %v, want %v", source, got, want)
		}
	}
}

func TestLoadConfigNeverHidesBothMeters(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := writeFile(path, []byte(`{"showInput":false,"showOutput":false}`)); err != nil {
		t.Fatal(err)
	}
	if c := LoadConfig(path); !c.ShowInput || c.ShowOutput {
		t.Fatalf("show input/output = %v/%v, want true/false", c.ShowInput, c.ShowOutput)
	}
}

//go:build windows || darwin

package main

import (
	"testing"

	"github.com/mareklanduch/gomic/internal/capture"
)

func TestPickerListsDefaultFirstAndChecksIt(t *testing.T) {
	devices := []capture.Device{{ID: "a", Name: "USB Mic"}, {ID: "b", Name: "Webcam"}}

	got := pickerEntries(devices, "USB Mic", capture.Device{})

	want := []pickerEntry{
		{label: "Default (USB Mic)", checked: true},
		{label: "USB Mic", device: devices[0]},
		{label: "Webcam", device: devices[1]},
	}
	if len(got) != len(want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("entry %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestPickerChecksTheChosenDevice(t *testing.T) {
	devices := []capture.Device{{ID: "a", Name: "USB Mic"}, {ID: "b", Name: "Webcam"}}

	got := pickerEntries(devices, "USB Mic", devices[1])

	if len(got) != 3 || got[0].checked || got[1].checked || !got[2].checked {
		t.Fatalf("got %+v, want only Webcam checked", got)
	}
}

func TestPickerKeepsAChosenDeviceThatIsNotConnectedVisible(t *testing.T) {
	devices := []capture.Device{{ID: "a", Name: "USB Mic"}}
	gone := capture.Device{ID: "z", Name: "Interface"}

	got := pickerEntries(devices, "", gone)

	want := pickerEntry{label: "Interface (not connected)", device: gone, checked: true, missing: true}
	if len(got) != 3 || got[0].label != "Default" || got[2] != want {
		t.Fatalf("got %+v, want last entry %+v", got, want)
	}
}

func TestHidingTheOnlyVisibleMeterShowsTheOther(t *testing.T) {
	cases := []struct {
		input, output bool
		toggle        capture.Source
		wantIn        bool
		wantOut       bool
	}{
		{true, false, capture.Output, true, true},
		{true, true, capture.Input, false, true},
		{true, true, capture.Output, true, false},
		{true, false, capture.Input, false, true},
		{false, true, capture.Output, true, false},
	}
	for _, c := range cases {
		in, out := toggledShow(c.input, c.output, c.toggle)
		if in != c.wantIn || out != c.wantOut {
			t.Errorf("toggle %v from %v/%v = %v/%v, want %v/%v", c.toggle, c.input, c.output, in, out, c.wantIn, c.wantOut)
		}
	}
}

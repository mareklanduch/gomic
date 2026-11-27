//go:build windows || darwin

package main

import (
	"time"

	"github.com/mareklanduch/gomic/internal/capture"
	"github.com/mareklanduch/gomic/internal/meter"
)

// meters is everything the window shows and remembers, shared by every
// platform's window code: one capturer and one display per source, indexed
// by capture.Input and capture.Output.
type meters struct {
	capt    [2]*capture.Capturer
	disp    [2]display
	cfg     Config
	cfgPath string
}

func newMeters(cfg Config, cfgPath string, now time.Time) *meters {
	m := &meters{cfg: cfg, cfgPath: cfgPath}
	chosen := [2]capture.Device{
		capture.Input:  {ID: cfg.InputDevice, Name: cfg.InputDeviceName},
		capture.Output: {ID: cfg.OutputDevice, Name: cfg.OutputDeviceName},
	}
	show := [2]bool{capture.Input: cfg.ShowInput, capture.Output: cfg.ShowOutput}
	for src := range m.capt {
		acc := &meter.Accumulator{}
		m.capt[src] = capture.New(acc, capture.Source(src), chosen[src], show[src])
		m.disp[src] = newDisplay(acc, now)
	}
	return m
}

// shown lists the visible meters, top to bottom.
func (m *meters) shown() []capture.Source {
	var s []capture.Source
	if m.cfg.ShowInput {
		s = append(s, capture.Input)
	}
	if m.cfg.ShowOutput {
		s = append(s, capture.Output)
	}
	return s
}

func (m *meters) tick(now time.Time) {
	for i := range m.disp {
		m.disp[i].tick(now)
	}
}

func (m *meters) resetClip() {
	for i := range m.disp {
		m.disp[i].resetClip()
	}
}

// toggleShow shows or hides one meter. Hiding the only visible meter shows
// the other one instead, so the window is never empty.
func (m *meters) toggleShow(src capture.Source) {
	m.cfg.ShowInput, m.cfg.ShowOutput = toggledShow(m.cfg.ShowInput, m.cfg.ShowOutput, src)
	show := [2]bool{capture.Input: m.cfg.ShowInput, capture.Output: m.cfg.ShowOutput}
	for i, c := range m.capt {
		if show[i] && !c.Enabled() {
			m.disp[i] = newDisplay(c.Acc, time.Now()) // start from silence, not from stale levels
		}
		c.SetEnabled(show[i])
	}
}

func toggledShow(input, output bool, src capture.Source) (bool, bool) {
	if src == capture.Input {
		input = !input
	} else {
		output = !output
	}
	if !input && !output {
		if src == capture.Input {
			output = true
		} else {
			input = true
		}
	}
	return input, output
}

// pick switches one meter to another device and remembers the choice.
func (m *meters) pick(src capture.Source, d capture.Device) {
	if d.ID == "" {
		d.Name = ""
	}
	if src == capture.Input {
		m.cfg.InputDevice, m.cfg.InputDeviceName = d.ID, d.Name
	} else {
		m.cfg.OutputDevice, m.cfg.OutputDeviceName = d.ID, d.Name
	}
	m.capt[src].Select(d)
	m.disp[src].resetClip()
}

// pickerEntry is one line of a device submenu.
type pickerEntry struct {
	label   string
	device  capture.Device
	checked bool
	missing bool // the chosen device is not connected; shown so the choice stays visible
}

// picker lists what the device submenu for src offers: the system default
// first, then every connected device.
func (m *meters) picker(src capture.Source) []pickerEntry {
	devices, defaultName := m.capt[src].Devices()
	return pickerEntries(devices, defaultName, m.capt[src].Selected())
}

func pickerEntries(devices []capture.Device, defaultName string, selected capture.Device) []pickerEntry {
	first := pickerEntry{label: "Default", checked: selected.ID == ""}
	if defaultName != "" {
		first.label = "Default (" + defaultName + ")"
	}
	entries := []pickerEntry{first}
	found := selected.ID == ""
	for _, d := range devices {
		entries = append(entries, pickerEntry{label: d.Name, device: d, checked: d.ID == selected.ID})
		found = found || d.ID == selected.ID
	}
	if !found {
		name := selected.Name
		if name == "" {
			name = "Chosen device"
		}
		entries = append(entries, pickerEntry{label: name + " (not connected)", device: selected, checked: true, missing: true})
	}
	return entries
}

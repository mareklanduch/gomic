//go:build windows || darwin

// gomic is a small always-on-top stereo peak meter with latching clip lights
// for an input device and (on Windows) an output device of your choice.
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/mareklanduch/gomic/internal/capture"
	"github.com/mareklanduch/gomic/internal/meter"
)

func main() {
	probe := flag.Duration("probe", 0, "print levels to the console for this long instead of opening the window")
	source := flag.String("source", "", `show "input", "output" or "both" meters (overrides the saved choice)`)
	flag.Parse()

	path := ConfigPath()
	cfg := LoadConfig(path)
	switch *source {
	case "":
	case "input", "output", "both":
		cfg.ShowInput, cfg.ShowOutput = *source != "output", *source != "input"
	default:
		fmt.Fprintf(os.Stderr, "gomic: -source must be \"input\", \"output\" or \"both\", not %q\n", *source)
		os.Exit(2)
	}
	if !capture.OutputSupported {
		if *source == "output" || *source == "both" {
			fmt.Fprintln(os.Stderr, "gomic: output metering is not available on this system")
			os.Exit(2)
		}
		cfg.ShowInput, cfg.ShowOutput = true, false
	}

	m := newMeters(cfg, path, time.Now())
	for _, c := range m.capt {
		go c.Run()
	}

	if *probe > 0 {
		runProbe(m, *probe)
		return
	}
	runWindow(m)
}

// runProbe prints what the meters would show, for checking capture without the window.
func runProbe(m *meters, d time.Duration) {
	end := time.Now().Add(d)
	for time.Now().Before(end) {
		time.Sleep(250 * time.Millisecond)
		for _, src := range m.shown() {
			name, err := m.capt[src].Status()
			if err != nil {
				fmt.Printf("%-6s error: %v\n", src, err)
				continue
			}
			peak, clipped := m.capt[src].Acc.Take()
			fmt.Printf("%-6s %-40s L %6.1f dB %-4s  R %6.1f dB %-4s\n", src, name,
				meter.DB(peak[0]), clipText(clipped[0]), meter.DB(peak[1]), clipText(clipped[1]))
		}
	}
}

func clipText(c bool) string {
	if c {
		return "CLIP"
	}
	return ""
}

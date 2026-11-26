//go:build windows || darwin

// gomic is a small always-on-top stereo peak meter with latching clip lights
// for the default input device (on Windows, optionally the default output).
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
	source := flag.String("source", "", `"input" or "output" (overrides the saved choice)`)
	flag.Parse()

	path := ConfigPath()
	cfg := LoadConfig(path)
	switch *source {
	case "":
	case "input", "output":
		cfg.Source = *source
	default:
		fmt.Fprintf(os.Stderr, "gomic: -source must be \"input\" or \"output\", not %q\n", *source)
		os.Exit(2)
	}

	acc := &meter.Accumulator{}
	capt := capture.New(acc, sourceFromName(cfg.Source))
	go capt.Run()

	if *probe > 0 {
		runProbe(acc, capt, *probe)
		return
	}
	runWindow(acc, capt, cfg, path)
}

func sourceFromName(name string) capture.Source {
	if name == "output" {
		return capture.Output
	}
	return capture.Input
}

func sourceName(s capture.Source) string {
	if s == capture.Output {
		return "output"
	}
	return "input"
}

// runProbe prints what the meter would show, for checking capture without the window.
func runProbe(acc *meter.Accumulator, capt *capture.Capturer, d time.Duration) {
	end := time.Now().Add(d)
	for time.Now().Before(end) {
		time.Sleep(250 * time.Millisecond)
		name, err := capt.Status()
		if err != nil {
			fmt.Println("error:", err)
			continue
		}
		peak, clipped := acc.Take()
		fmt.Printf("%-40s L %6.1f dB %-4s  R %6.1f dB %-4s\n", name,
			meter.DB(peak[0]), clipText(clipped[0]), meter.DB(peak[1]), clipText(clipped[1]))
	}
}

func clipText(c bool) string {
	if c {
		return "CLIP"
	}
	return ""
}

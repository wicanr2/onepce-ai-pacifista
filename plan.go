package onepce

import "github.com/wicanr2/onepce-ai-remake/internal/machine"

// ButtonByName maps the names used by the oracle probes and the CLI
// (i, ii, select, run, up, right, down, left) to button bits.
func ButtonByName(name string) (uint8, error) { return machine.ButtonByName(name) }

// ButtonName is the inverse of ButtonByName for a single-bit value.
func ButtonName(b uint8) string { return machine.ButtonName(b) }

// ParsePresses reads the "frame:button:span,…" form shared with the Mesen2
// probes (STATE_PRESS), so one string drives both the oracle and this machine.
func ParsePresses(text string) ([]Press, error) { return machine.ParsePlan(text) }

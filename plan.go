package onepce

import (
	"fmt"
	"strconv"
	"strings"
)

// ButtonByName maps the names used by the oracle probes and the CLI
// (i, ii, select, run, up, right, down, left) to button bits.
func ButtonByName(name string) (uint8, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "i":
		return ButtonI, nil
	case "ii":
		return ButtonII, nil
	case "select":
		return ButtonSelect, nil
	case "run":
		return ButtonRun, nil
	case "up":
		return ButtonUp, nil
	case "right":
		return ButtonRight, nil
	case "down":
		return ButtonDown, nil
	case "left":
		return ButtonLeft, nil
	}
	return 0, fmt.Errorf("unknown button %q", name)
}

// ButtonName is the inverse of ButtonByName for a single-bit value.
func ButtonName(b uint8) string {
	switch b {
	case ButtonI:
		return "i"
	case ButtonII:
		return "ii"
	case ButtonSelect:
		return "select"
	case ButtonRun:
		return "run"
	case ButtonUp:
		return "up"
	case ButtonRight:
		return "right"
	case ButtonDown:
		return "down"
	case ButtonLeft:
		return "left"
	}
	return fmt.Sprintf("0x%02X", b)
}

// ParsePresses reads the "frame:button:span,…" form shared with the Mesen2
// probes (STATE_PRESS), so one string drives both the oracle and this machine.
func ParsePresses(text string) ([]Press, error) {
	var out []Press
	for _, item := range strings.Split(text, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		parts := strings.Split(item, ":")
		if len(parts) != 3 {
			return nil, fmt.Errorf("press %q: want frame:button:span", item)
		}
		frame, err := strconv.ParseUint(parts[0], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("press %q: frame: %w", item, err)
		}
		button, err := ButtonByName(parts[1])
		if err != nil {
			return nil, fmt.Errorf("press %q: %w", item, err)
		}
		span, err := strconv.Atoi(parts[2])
		if err != nil || span <= 0 {
			return nil, fmt.Errorf("press %q: span must be a positive integer", item)
		}
		out = append(out, Press{Frame: frame, Button: button, Span: span})
	}
	return out, nil
}

package bus

import "github.com/wicanr2/onepce-ai-remake/internal/addr"

// TimerState is the serialisable timer.
type TimerState struct {
	Enabled bool
	Reload  uint8
	Counter uint8
	Scaler  int
}

// State is the serialisable bus (docs/spec/state.md S4). The ROM is not
// included: the loader checks its hash instead.
type State struct {
	MPR      addr.MPR
	MPRLast  uint8
	Fast     bool
	IRQMask  uint8
	IRQLines uint8
	IOBuffer uint8
	Master   uint64
	RAM      [bankSize]uint8
	Timer    TimerState
}

// Save copies the bus state out.
func (b *Bus) Save() State {
	t := b.dev.Timer
	return State{MPR: b.mpr, MPRLast: b.mprLast, Fast: b.fast, IRQMask: b.irqMask, IRQLines: b.irqLines,
		IOBuffer: b.ioBuffer, Master: b.master, RAM: b.RAM,
		Timer: TimerState{Enabled: t.enabled, Reload: t.reload, Counter: t.counter, Scaler: t.scaler}}
}

// Restore loads the bus state; devices and hooks stay attached.
func (b *Bus) Restore(s State) {
	b.mpr, b.mprLast, b.fast = s.MPR, s.MPRLast, s.Fast
	b.irqMask, b.irqLines, b.ioBuffer = s.IRQMask, s.IRQLines, s.IOBuffer
	b.master = s.Master
	b.RAM = s.RAM
	t := b.dev.Timer
	t.enabled, t.reload, t.counter, t.scaler = s.Timer.Enabled, s.Timer.Reload, s.Timer.Counter, s.Timer.Scaler
}

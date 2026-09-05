package machine

import (
	"github.com/wicanr2/onepce-ai-pacifista/internal/bus"
	"github.com/wicanr2/onepce-ai-pacifista/internal/huc6280"
	"github.com/wicanr2/onepce-ai-pacifista/internal/psg"
	"github.com/wicanr2/onepce-ai-pacifista/internal/vce"
	"github.com/wicanr2/onepce-ai-pacifista/internal/vdc"
)

// PadState is the serialisable controller port.
type PadState struct {
	Held uint8
	Sel  bool
	Clr  bool
}

// State is the whole console (docs/spec/state.md S4).
type State struct {
	CPU        huc6280.State
	Bus        bus.State
	VDC        vdc.State
	VCE        vce.State
	PSG        psg.Saved
	Pad        PadState
	LastMaster uint64
	Plan       []Press
	ReleaseAt  [8]uint64
}

// Save copies every component out.
func (m *Machine) Save() State {
	return State{
		CPU: m.CPU.Save(), Bus: m.Bus.Save(), VDC: m.VDC.Save(), VCE: m.VCE.Save(), PSG: m.PSG.Save(),
		Pad:        PadState{Held: m.Pad.Held, Sel: m.Pad.sel, Clr: m.Pad.clr},
		LastMaster: m.lastMaster,
		Plan:       append([]Press(nil), m.plan...),
		ReleaseAt:  m.releaseAt,
	}
}

// Restore loads every component; bindings and hooks stay attached.
func (m *Machine) Restore(s State) {
	m.CPU.Restore(s.CPU)
	m.Bus.Restore(s.Bus)
	m.VDC.Restore(s.VDC)
	m.VCE.Restore(s.VCE)
	m.PSG.Load(s.PSG)
	m.Pad.Held, m.Pad.sel, m.Pad.clr = s.Pad.Held, s.Pad.Sel, s.Pad.Clr
	m.lastMaster = s.LastMaster
	m.plan = append([]Press(nil), s.Plan...)
	m.releaseAt = s.ReleaseAt
}

// Package machine assembles a HuCard PC Engine: CPU, bus, VDC, VCE and the
// controller port, and steps them together. It is the only place that knows
// how the parts are clocked against each other.
package machine

import (
	"fmt"

	"github.com/wicanr2/onepce-ai-remake/internal/bus"
	"github.com/wicanr2/onepce-ai-remake/internal/huc6280"
	"github.com/wicanr2/onepce-ai-remake/internal/vce"
	"github.com/wicanr2/onepce-ai-remake/internal/vdc"
)

// Button bits of the two-button pad, in the order the port reports them.
const (
	ButtonI      uint8 = 0x01
	ButtonII     uint8 = 0x02
	ButtonSelect uint8 = 0x04
	ButtonRun    uint8 = 0x08
	ButtonUp     uint8 = 0x10
	ButtonRight  uint8 = 0x20
	ButtonDown   uint8 = 0x40
	ButtonLeft   uint8 = 0x80
)

// Pad is the standard two-button controller on port 1. The game strobes SEL
// (bit 0 of the port) to pick which nibble it reads: SEL low gives
// I/II/SELECT/RUN, SEL high the four directions; a pressed button reads as 0.
// CLR (bit 1) high forces the nibble to 0.
// 參考行為：Mesen2 Input/PceController.h @ b9fa69d（行為事實）。
type Pad struct {
	Held uint8
	sel  bool
	clr  bool
}

func (p *Pad) Read(_ uint16) uint8 {
	if p.clr {
		return 0
	}
	if p.sel {
		return ^(p.Held >> 4) & 0x0F
	}
	return ^p.Held & 0x0F
}

func (p *Pad) Write(_ uint16, value uint8) {
	p.sel = value&0x01 != 0
	p.clr = value&0x02 != 0
}

type irqLine struct{ b *bus.Bus }

func (l irqLine) Assert() { l.b.AssertIRQ1() }
func (l irqLine) Clear()  { l.b.ClearIRQ1() }

// Press is one scheduled input: hold Button from frame Frame for Span
// frames. Frames are the VDC's frame counter, the same count the Mesen2
// oracle reports at its end-of-frame callback, so a plan can be replayed on
// both (spec docs/spec/machine.md).
type Press struct {
	Frame  uint64
	Button uint8
	Span   int
}

// Machine is one console with a cartridge inserted.
type Machine struct {
	CPU *huc6280.CPU
	Bus *bus.Bus
	VDC *vdc.VDC
	VCE *vce.VCE
	Pad *Pad

	lastMaster uint64
	plan       []Press
	releaseAt  [8]uint64 // per button bit: frame at which it is released

	// FrameHook, when set, is called from inside the CPU cycle in which the
	// VDC's frame counter advances — the same instant the oracle's
	// end-of-frame callback runs — so a snapshot taken there is comparable
	// cycle for cycle.
	FrameHook func(frame uint64)
}

// Schedule adds presses to the input plan. They take effect at the frame
// boundary of their Frame, exactly as the oracle's Lua probe applies them.
func (m *Machine) Schedule(presses ...Press) {
	m.plan = append(m.plan, presses...)
}

// applyPlan runs at every frame boundary (VDC frame counter just advanced).
func (m *Machine) applyPlan() {
	frame := m.VDC.Frame()
	for _, p := range m.plan {
		if p.Frame == frame {
			for bit := 0; bit < 8; bit++ {
				if p.Button&(1<<bit) != 0 {
					m.releaseAt[bit] = frame + uint64(p.Span)
				}
			}
		}
	}
	var held uint8
	for bit := 0; bit < 8; bit++ {
		if frame < m.releaseAt[bit] {
			held |= 1 << bit
		}
	}
	m.Pad.Held = held
}

// New builds a machine around a ROM image and resets it.
func New(rom []byte) (*Machine, error) {
	m := &Machine{VCE: vce.New(), Pad: &Pad{}}
	b, err := bus.New(rom, bus.Devices{VCE: m.VCE, Pad: m.Pad})
	if err != nil {
		return nil, fmt.Errorf("machine: %w", err)
	}
	m.Bus = b
	m.VDC = vdc.New(m.VCE, irqLine{b})
	if err := b.Attach(bus.Devices{VDC: m.VDC, VCE: m.VCE, Pad: m.Pad}); err != nil {
		return nil, err
	}
	// Every CPU cycle moves the VDC forward, so a status read in the middle of
	// an instruction sees the video state of that very cycle.
	b.Clock = func(master uint64) {
		before := m.VDC.Frame()
		m.VDC.Advance(master - m.lastMaster)
		m.lastMaster = master
		if m.FrameHook != nil && m.VDC.Frame() != before {
			m.FrameHook(m.VDC.Frame())
		}
	}
	m.CPU = huc6280.New(b)
	m.CPU.Reset()
	return m, nil
}

// Step executes one CPU instruction; the video side follows cycle by cycle.
func (m *Machine) Step() int {
	return m.CPU.Step()
}

// RunFrame steps until the VDC reports a frame boundary, then applies the
// input plan for the new frame.
func (m *Machine) RunFrame() {
	for !m.VDC.TakeFrameReady() {
		m.Step()
	}
	m.applyPlan()
}

// RunToFrame runs until the VDC frame counter reaches frame.
func (m *Machine) RunToFrame(frame uint64) {
	for m.VDC.Frame() < frame {
		m.RunFrame()
	}
}

// RunFrames runs n frames.
func (m *Machine) RunFrames(n int) {
	for i := 0; i < n; i++ {
		m.RunFrame()
	}
}

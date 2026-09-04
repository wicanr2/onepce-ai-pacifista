package onepce

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	"image/color"

	"github.com/wicanr2/onepce-ai-remake/internal/huc6280"
	"github.com/wicanr2/onepce-ai-remake/internal/machine"
	"github.com/wicanr2/onepce-ai-remake/internal/psg"
	"github.com/wicanr2/onepce-ai-remake/internal/vce"
	"github.com/wicanr2/onepce-ai-remake/internal/vdc"
)

// Version is stamped into snapshots and savestates.
const Version = "0.1.0-m5"

// Buttons of the two-button pad, usable as a bit set in Press.
const (
	ButtonI      = machine.ButtonI
	ButtonII     = machine.ButtonII
	ButtonSelect = machine.ButtonSelect
	ButtonRun    = machine.ButtonRun
	ButtonUp     = machine.ButtonUp
	ButtonRight  = machine.ButtonRight
	ButtonDown   = machine.ButtonDown
	ButtonLeft   = machine.ButtonLeft
)

// Press holds Button from frame Frame for Span frames (docs/spec/machine.md M3).
type Press = machine.Press

// Kind of a watch / event.
type Kind uint8

const (
	Read Kind = iota
	Write
	Exec
)

// Space an address belongs to.
type Space uint8

const (
	CPU  Space = iota // CPU logical address
	VRAM              // VDC VRAM word address; SATB writes report the SAT index
)

// Source of a VRAM write.
type Source uint8

const (
	ByCPU  Source = Source(vdc.ByCPU)
	ByDMA  Source = Source(vdc.ByDMA)
	BySATB Source = Source(vdc.BySATB)
)

// Event is one watch hit (docs/spec/observe.md O1).
type Event struct {
	Kind          Kind
	Space         Space
	Source        Source
	PC            uint16 // instruction start
	Opcode        uint8
	Addr          Address // for VRAM events Logical/Physical hold the word address, File is unknown
	Value         uint16
	Frame         uint64
	Scanline      int
	HClock        int
	Cycles        uint64
	A, X, Y, S, P uint8
}

// Machine is the public face of one console (docs/spec/observe.md).
type Machine struct {
	m       *machine.Machine
	romHash string
	plan    []Press
	watches []*Watch
	traces  []*traceHook
	nextID  int
	vgm     *psg.Recorder
}

// Load builds a machine around a HuCard image and resets it.
func Load(rom []byte) (*Machine, error) {
	inner, err := machine.New(rom)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(rom)
	pm := &Machine{m: inner, romHash: hex.EncodeToString(sum[:])}
	inner.Bus.OnRead = func(a uint16, v uint8) { pm.dispatchCPU(Read, a, uint16(v)) }
	inner.Bus.OnWrite = func(a uint16, v uint8) { pm.dispatchCPU(Write, a, uint16(v)) }
	inner.VDC.OnVRAMWrite = func(a, v uint16, src vdc.Source) { pm.dispatchVRAM(a, v, Source(src)) }
	inner.InstructionHook = pm.onInstruction
	return pm, nil
}

// ROMHash is the SHA-256 of the loaded image.
func (pm *Machine) ROMHash() string { return pm.romHash }

// Schedule adds input presses (frames are the VDC frame counter).
func (pm *Machine) Schedule(presses ...Press) {
	pm.plan = append(pm.plan, presses...)
	pm.m.Schedule(presses...)
}

// Plan returns the input plan so far (recorded into snapshots).
func (pm *Machine) Plan() []Press { return append([]Press(nil), pm.plan...) }

// Step runs one instruction.
func (pm *Machine) Step() int { return pm.m.Step() }

// RunFrames runs n frames.
func (pm *Machine) RunFrames(n int) { pm.m.RunFrames(n) }

// RunToFrame runs until the frame counter reaches frame.
func (pm *Machine) RunToFrame(frame uint64) { pm.m.RunToFrame(frame) }

// Frame is the VDC frame counter.
func (pm *Machine) Frame() uint64 { return pm.m.VDC.Frame() }

// Registers is the CPU register view right now.
func (pm *Machine) Registers() huc6280.Snapshot { return pm.m.CPU.Peek() }

// MPR is the paging state right now.
func (pm *Machine) MPR() MPR { return pm.m.Bus.MPR() }

// Resolve maps a logical address through the current paging state and mapper.
func (pm *Machine) Resolve(logical uint16) Address { return pm.m.Bus.Resolve(logical) }

// Peek reads CPU space without side effects.
func (pm *Machine) Peek(logical uint16) uint8 { return pm.m.Bus.Peek(logical) }

// Internal exposes the assembled machine for the CLI/RPC layers; test code
// in other packages should prefer the methods above.
func (pm *Machine) Internal() *machine.Machine { return pm.m }

// --- watches ---

// Watch is one address-range watchpoint (docs/spec/observe.md O2–O4).
type Watch struct {
	pm       *Machine
	id       int
	kind     Kind
	space    Space
	lo, hi   uint32
	fn       func(Event)
	limit    int
	count    int
	skipped  int
	ignored  int
	ignorePC map[uint16]bool
	removed  bool
}

const defaultLimit = 10000

// Watch registers fn for kind accesses in space within [lo, hi].
func (pm *Machine) Watch(kind Kind, space Space, lo, hi uint32, fn func(Event)) *Watch {
	pm.nextID++
	w := &Watch{pm: pm, id: pm.nextID, kind: kind, space: space, lo: lo, hi: hi, fn: fn, limit: defaultLimit}
	pm.watches = append(pm.watches, w)
	return w
}

// Limit caps the number of recorded events; further hits are counted as skipped.
func (w *Watch) Limit(n int) *Watch { w.limit = n; return w }

// IgnorePC drops hits whose instruction started at one of these addresses.
func (w *Watch) IgnorePC(pcs ...uint16) *Watch {
	if w.ignorePC == nil {
		w.ignorePC = map[uint16]bool{}
	}
	for _, pc := range pcs {
		w.ignorePC[pc] = true
	}
	return w
}

// Count / Skipped / Ignored are the bookkeeping a caller must report.
func (w *Watch) Count() int   { return w.count }
func (w *Watch) Skipped() int { return w.skipped }
func (w *Watch) Ignored() int { return w.ignored }

// Remove detaches the watch.
func (w *Watch) Remove() {
	w.removed = true
	for i, x := range w.pm.watches {
		if x == w {
			w.pm.watches = append(w.pm.watches[:i], w.pm.watches[i+1:]...)
			return
		}
	}
}

func (pm *Machine) event(kind Kind, space Space, src Source, addr Address, value uint16) Event {
	c := pm.m.CPU
	return Event{
		Kind: kind, Space: space, Source: src, PC: c.InstPC, Opcode: pm.m.Bus.Peek(c.InstPC),
		Addr: addr, Value: value, Frame: pm.m.VDC.Frame(), Scanline: pm.m.VDC.Scanline(),
		HClock: pm.m.VDC.HClock(), Cycles: c.Cycles, A: c.A, X: c.X, Y: c.Y, S: c.S, P: c.P,
	}
}

func (w *Watch) deliver(ev Event) {
	if w.ignorePC != nil && w.ignorePC[ev.PC] {
		w.ignored++
		return
	}
	if w.count >= w.limit {
		w.skipped++
		return
	}
	w.count++
	w.fn(ev)
}

func (pm *Machine) dispatchCPU(kind Kind, logical uint16, value uint16) {
	if len(pm.watches) == 0 {
		return
	}
	var ev Event
	built := false
	for _, w := range pm.watches {
		if w.kind != kind || w.space != CPU || uint32(logical) < w.lo || uint32(logical) > w.hi {
			continue
		}
		if !built {
			ev = pm.event(kind, CPU, ByCPU, pm.m.Bus.Resolve(logical), value)
			built = true
		}
		w.deliver(ev)
	}
}

func (pm *Machine) dispatchVRAM(word uint16, value uint16, src Source) {
	if len(pm.watches) == 0 {
		return
	}
	var ev Event
	built := false
	for _, w := range pm.watches {
		if w.kind != Write || w.space != VRAM || uint32(word) < w.lo || uint32(word) > w.hi {
			continue
		}
		if !built {
			ev = pm.event(Write, VRAM, src, Address{Logical: word, Physical: uint32(word), File: FileUnknown, MPR: pm.m.Bus.MPR()}, value)
			built = true
		}
		w.deliver(ev)
	}
}

func (pm *Machine) onInstruction(snap huc6280.Snapshot) {
	for _, tr := range pm.traces {
		tr.fn(snap)
	}
	if len(pm.watches) == 0 {
		return
	}
	var ev Event
	built := false
	for _, w := range pm.watches {
		if w.kind != Exec || w.space != CPU || uint32(snap.PC) < w.lo || uint32(snap.PC) > w.hi {
			continue
		}
		if !built {
			ev = pm.event(Exec, CPU, ByCPU, pm.m.Bus.Resolve(snap.PC), uint16(snap.Opcode))
			ev.PC = snap.PC
			ev.Opcode = snap.Opcode
			built = true
		}
		w.deliver(ev)
	}
}

// --- trace ---

type traceHook struct{ fn func(huc6280.Snapshot) }

// Trace calls fn before every instruction; the returned func detaches it.
func (pm *Machine) Trace(fn func(huc6280.Snapshot)) func() {
	tr := &traceHook{fn: fn}
	pm.traces = append(pm.traces, tr)
	return func() {
		for i, x := range pm.traces {
			if x == tr {
				pm.traces = append(pm.traces[:i], pm.traces[i+1:]...)
				return
			}
		}
	}
}

// TraceHash accumulates the PC+opcode structure hash of everything executed
// while it is attached (docs/spec/observe.md O6).
type TraceHash struct {
	h interface {
		Write([]byte) (int, error)
		Sum([]byte) []byte
	}
	count  int
	detach func()
}

// NewTraceHash attaches a structure-hash trace to the machine.
func (pm *Machine) NewTraceHash() *TraceHash {
	th := &TraceHash{h: sha256.New()}
	th.detach = pm.Trace(func(s huc6280.Snapshot) {
		fmt.Fprintf(th.h, "%04X%02X", s.PC, s.Opcode)
		th.count++
	})
	return th
}

// Sum returns the hex digest and instruction count so far.
func (th *TraceHash) Sum() (string, int) { return hex.EncodeToString(th.h.Sum(nil)), th.count }

// Detach stops recording.
func (th *TraceHash) Detach() { th.detach() }

// --- framebuffer ---

// FramebufferNative returns the VDC's display window as 9-bit VCE colours.
func (pm *Machine) FramebufferNative() (w, h int, px []uint16) { return pm.m.VDC.Framebuffer() }

// --- audio (docs/spec/psg.md §5) ---

// SetAudioRate starts producing interleaved stereo samples at rate Hz;
// 0 stops. Samples accumulate until DrainAudio takes them.
func (pm *Machine) SetAudioRate(rate int) { pm.m.PSG.SetSampleRate(rate) }

// DrainAudio returns the samples produced since the last call.
func (pm *Machine) DrainAudio() []int16 { return pm.m.PSG.Drain() }

// PSGState is the sound chip's registers and counters.
func (pm *Machine) PSGState() psg.State { return pm.m.PSG.State }

// RecordVGM records PSG port writes made while the start-of-frame counter is
// in [start, stop) into a VGM stream (spec psg.md §3). Only one recording at
// a time; a new call replaces the previous one.
func (pm *Machine) RecordVGM(start, stop uint64) {
	pm.vgm = psg.NewRecorder(start, stop)
	pm.m.PSG.OnWrite = pm.vgm.Write
	pm.m.VDC.OnStartFrame = pm.vgm.StartFrame
}

// VGM returns the recording so far and whether its window has closed.
func (pm *Machine) VGM() ([]byte, bool) {
	if pm.vgm == nil {
		return nil, false
	}
	return pm.vgm.Bytes(), pm.vgm.Done()
}

// DisplayWindow reports where FramebufferNative's (0,0) sits in the oracle's
// picture coordinates: display-start dot and the scanline of VDW raster 0
// (docs/spec/framebuffer-parity.md §3).
func (pm *Machine) DisplayWindow() (dot0, line0 int) { return pm.m.VDC.DisplayWindow() }

// ClockDivider is the VCE dot-clock divider in effect (4, 3 or 2).
func (pm *Machine) ClockDivider() int { return pm.m.VCE.ClockDivider() }

// Framebuffer expands the native picture to RGBA at native resolution
// (docs/spec/observe.md O8: no aspect correction).
func (pm *Machine) Framebuffer() *image.RGBA {
	w, h, px := pm.m.VDC.Framebuffer()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i, c := range px {
		r, g, b := vce.RGB(c)
		img.SetRGBA(i%w, i/w, color.RGBA{R: r, G: g, B: b, A: 255})
	}
	return img
}

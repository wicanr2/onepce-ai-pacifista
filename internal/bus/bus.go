// Package bus is the 21-bit physical address space of a HuCard PC Engine:
// ROM banks with their mirroring, the 8 KiB work RAM, the paging registers
// and the I/O page. Spec: docs/spec/hucard-mapper.md and huc6280.md C10/C11.
//
// 參考行為：Mesen2 PceMemoryManager.cpp @ b9fa69d §non-power-of-two mirroring、
// §I/O page dispatch（只取行為事實）。
package bus

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/wicanr2/onepce-ai-pacifista/internal/addr"
)

const (
	bankSize = 0x2000
	ramBank  = 0xF8 // $F8–$FB mirror the 8 KiB work RAM
	ioBank   = 0xFF
)

// Devices are the memory-mapped peripherals on the I/O page. The bus does not
// know their internals; M1 uses stubs, M2 plugs the real VDC/VCE in.
type Devices struct {
	VDC   Device // $0000–$03FF, and ST0/ST1/ST2
	VCE   Device // $0400–$07FF
	PSG   Device // $0800–$0BFF
	Pad   Device // $1000–$13FF
	Timer *Timer // $0C00–$0FFF (owned here: it is clocked by the CPU)
}

// Device is a memory-mapped peripheral addressed by the offset inside its
// 1 KiB window.
type Device interface {
	Read(offset uint16) uint8
	Write(offset uint16, value uint8)
}

// Bus is the physical address space. Zero value is not usable; use New.
type Bus struct {
	ROM     []byte
	ROMHash string
	RAM     [bankSize]uint8
	mpr     addr.MPR
	mprLast uint8
	// romBank[i] is the ROM bank presented in physical bank i (0..$7F), or -1.
	romBank  [0x80]int
	dev      Devices
	fast     bool
	irqMask  uint8 // $1402: 1 = disabled (bit0 IRQ2, bit1 IRQ1, bit2 timer)
	irqLines uint8 // asserted lines, same bits
	ioBuffer uint8 // last value on the I/O data bus (read back by PSG/timer/IRQ regs)
	master   uint64
	// Clock, when set, is called after every tick with the master clock so
	// the video side can be kept in step with each CPU cycle.
	Clock func(master uint64)
	// sample is the CPU's per-cycle interrupt sample (huc6280.Bus.SetIRQSampler).
	sample func()
	// OnRead / OnWrite, when set, see every CPU-space access with its value
	// (reads after the value is known). They are the observe layer's taps
	// (docs/spec/observe.md O2) and must not touch the bus.
	OnRead  func(logical uint16, value uint8)
	OnWrite func(logical uint16, value uint8)
}

// New maps a HuCard image. The image must be a whole number of 8 KiB banks.
func New(rom []byte, dev Devices) (*Bus, error) {
	if len(rom) == 0 || len(rom)%bankSize != 0 {
		return nil, fmt.Errorf("bus: ROM is %d bytes, not a multiple of 8 KiB", len(rom))
	}
	sum := sha256.Sum256(rom)
	b := &Bus{ROM: rom, ROMHash: hex.EncodeToString(sum[:]), dev: dev}
	if b.dev.Timer == nil {
		b.dev.Timer = &Timer{}
	}
	b.mapROM()
	b.Reset()
	return b, nil
}

// Attach plugs devices in after construction (the VDC needs the bus for its
// interrupt line, so it cannot exist before the bus does). Nil fields are
// left as they were.
func (b *Bus) Attach(dev Devices) error {
	if dev.VDC != nil {
		b.dev.VDC = dev.VDC
	}
	if dev.VCE != nil {
		b.dev.VCE = dev.VCE
	}
	if dev.PSG != nil {
		b.dev.PSG = dev.PSG
	}
	if dev.Pad != nil {
		b.dev.Pad = dev.Pad
	}
	if dev.Timer != nil {
		b.dev.Timer = dev.Timer
	}
	return nil
}

// Reset is the power-on state of spec B6.
func (b *Bus) Reset() {
	b.mpr = addr.MPR{}
	b.mprLast = 0
	b.fast = false
	b.irqMask = 0
	b.irqLines = 0
	b.ioBuffer = 0
	b.master = 0
	b.dev.Timer.reset()
}

// mapROM fills romBank per spec B2–B4.
func (b *Bus) mapROM() {
	n := len(b.ROM) / bankSize
	// Each of the eight 16-bank groups starts at a ROM bank chosen by size.
	var groups [8]int
	switch n {
	case 0x30: // 384 KiB: 256 ×2 then 128 ×4
		groups = [8]int{0x00, 0x10, 0x00, 0x10, 0x20, 0x20, 0x20, 0x20}
	case 0x40: // 512 KiB
		groups = [8]int{0x00, 0x10, 0x20, 0x30, 0x20, 0x30, 0x20, 0x30}
	case 0x60: // 768 KiB
		groups = [8]int{0x00, 0x10, 0x20, 0x30, 0x40, 0x50, 0x40, 0x50}
	default:
		groups = [8]int{0x00, 0x10, 0x20, 0x30, 0x40, 0x50, 0x60, 0x70}
	}
	for i := range b.romBank {
		b.romBank[i] = (groups[i>>4] + (i & 0x0F)) % n
	}
}

// MPR returns a copy of the paging registers for events and snapshots.
func (b *Bus) MPR() addr.MPR { return b.mpr }

// Physical maps a logical address under the current paging state.
func (b *Bus) Physical(logical uint16) uint32 { return b.mpr.Physical(logical) }

// FileOffset is the ROM file offset behind a physical address, or
// addr.FileUnknown for RAM, I/O and unmapped banks (spec B5).
func (b *Bus) FileOffset(physical uint32) int64 {
	bank := physical >> 13
	if bank >= 0x80 {
		return addr.FileUnknown
	}
	return int64(b.romBank[bank])*bankSize + int64(physical&0x1FFF)
}

// Resolve is the three-space view of a logical address right now.
func (b *Bus) Resolve(logical uint16) addr.Address {
	a := addr.Resolve(logical, b.mpr)
	a.File = b.FileOffset(a.Physical)
	return a
}

// --- huc6280.Bus ---

// Peek reads without clocking or side effects (debuggers, trace, tests).
// I/O reads are not simulated: the page reads as $FF.
func (b *Bus) Peek(logical uint16) uint8 {
	bank := b.mpr.Bank(logical)
	off := logical & 0x1FFF
	switch {
	case bank < 0x80:
		return b.ROM[b.romBank[bank]*bankSize+int(off)]
	case bank >= ramBank && bank <= ramBank+3:
		return b.RAM[off]
	}
	return 0xFF
}

// SetIRQSampler installs the CPU's interrupt sample (huc6280.Bus).
func (b *Bus) SetIRQSampler(fn func()) { b.sample = fn }

// cpuCycle is one CPU cycle: the clock moves, then the CPU samples its
// interrupt lines, then (in the caller) the access happens. Oracle order:
// Mesen2 PceCpu::ProcessCpuCycle @ b9fa69d (behaviour fact only).
func (b *Bus) cpuCycle() {
	b.Tick(1)
	if b.sample != nil {
		b.sample()
	}
}

// Poke writes work RAM through the paging registers without clocking
// anything or touching I/O: an experiment's hand on the machine (spec
// observe.md O10). Only RAM banks accept it; the result says whether it landed.
func (b *Bus) Poke(logical uint16, value uint8) bool {
	bank := b.mpr.Bank(logical)
	if bank >= ramBank && bank <= ramBank+3 {
		b.RAM[logical&0x1FFF] = value
		return true
	}
	return false
}

// Idle is one CPU cycle with no bus access.
func (b *Bus) Idle() { b.cpuCycle() }

func (b *Bus) Read(logical uint16) uint8 {
	b.cpuCycle()
	v := b.readRaw(logical)
	if b.OnRead != nil {
		b.OnRead(logical, v)
	}
	return v
}

func (b *Bus) readRaw(logical uint16) uint8 {
	bank := b.mpr.Bank(logical)
	off := logical & 0x1FFF
	switch {
	case bank < 0x80:
		return b.ROM[b.romBank[bank]*bankSize+int(off)]
	case bank >= ramBank && bank <= ramBank+3:
		return b.RAM[off]
	case bank == ioBank:
		return b.readIO(off)
	}
	return 0xFF
}

func (b *Bus) Write(logical uint16, value uint8) {
	b.cpuCycle()
	if b.OnWrite != nil {
		b.OnWrite(logical, value)
	}
	bank := b.mpr.Bank(logical)
	off := logical & 0x1FFF
	switch {
	case bank >= ramBank && bank <= ramBank+3:
		b.RAM[off] = value
	case bank == ioBank:
		b.writeIO(off, value)
	}
}

func (b *Bus) WriteVDCPort(port uint8, value uint8) {
	b.cpuCycle() // the write cycle itself
	if b.dev.VDC != nil {
		b.dev.VDC.Write(uint16(port), value)
	}
	b.cpuCycle() // the VDC access stalls the CPU one more cycle (spec C10)
}

func (b *Bus) SetMPR(mask, value uint8) {
	if mask == 0 {
		return
	}
	b.mprLast = value
	for i := 0; i < 8; i++ {
		if mask&(1<<i) != 0 {
			b.mpr[i] = value
		}
	}
}

func (b *Bus) GetMPR(mask uint8) uint8 {
	if mask == 0 {
		return b.mprLast
	}
	var v uint8
	for i := 0; i < 8; i++ {
		if mask&(1<<i) != 0 {
			v |= b.mpr[i]
		}
	}
	b.mprLast = v
	return v
}

func (b *Bus) SetSpeed(fast bool) { b.fast = fast }

// Fast reports whether the CPU is in CSH mode.
func (b *Bus) Fast() bool { return b.fast }

// Tick advances the CPU-clocked devices. Master clock: 3 per CPU cycle at
// high speed, 12 at low speed (spec C6).
func (b *Bus) Tick(cycles int) {
	if b.fast {
		b.master += uint64(cycles) * 3
	} else {
		b.master += uint64(cycles) * 12
	}
	// The timer is clocked from the master clock, three clocks per step,
	// so a slow-speed CPU cycle advances it four steps (spec C5).
	steps := cycles
	if !b.fast {
		steps = cycles * 4
	}
	if b.dev.Timer.tick(steps) {
		b.irqLines |= irqTimer
	}
	if b.Clock != nil {
		b.Clock(b.master)
	}
}

// StallStep is one VDC tick (three master clocks) during which the CPU
// waits on the VDC's VRAM access queue (docs/spec/vdc-vce.md §5.1): the
// timer and the clock hook advance, the CPU does not.
func (b *Bus) StallStep() {
	b.master += 3
	if b.dev.Timer.tick(1) {
		b.irqLines |= irqTimer
	}
	if b.Clock != nil {
		b.Clock(b.master)
	}
}

// MasterCycles is the elapsed master clock, for the VDC/VCE side (M2).
func (b *Bus) MasterCycles() uint64 { return b.master }

const (
	irqIRQ2  uint8 = 0x01
	irqIRQ1  uint8 = 0x02
	irqTimer uint8 = 0x04
)

func (b *Bus) PendingIRQ() uint8 { return b.irqLines &^ b.irqMask }

// AssertIRQ1 / ClearIRQ1 are the VDC's interrupt line (M2). IRQ2 is the
// CD-ROM / expansion line and is never asserted on a plain HuCard.
func (b *Bus) AssertIRQ1() { b.irqLines |= irqIRQ1 }
func (b *Bus) ClearIRQ1()  { b.irqLines &^= irqIRQ1 }

// --- I/O page (spec C10/C11) ---

func (b *Bus) readIO(off uint16) uint8 {
	switch {
	case off < 0x0400:
		b.Tick(1)
		if b.dev.VDC != nil {
			return b.dev.VDC.Read(off & 0x03)
		}
		return 0xFF
	case off < 0x0800:
		b.Tick(1)
		if b.dev.VCE != nil {
			return b.dev.VCE.Read(off & 0x07)
		}
		return 0xFF
	case off < 0x0C00:
		return b.ioBuffer
	case off < 0x1000:
		b.ioBuffer = b.ioBuffer&0x80 | b.dev.Timer.read()&0x7F
		return b.ioBuffer
	case off < 0x1400:
		var pad uint8 = 0x0F
		if b.dev.Pad != nil {
			pad = b.dev.Pad.Read(0) & 0x0F
		}
		// bits 5–4 always read as set; bit7 (CD-ROM absent) and bit6 (region:
		// TurboGrafx=0) are reported as 0, which is what the Mesen2 oracle
		// reports in its default configuration (spec C11: to be settled by
		// the Nectaris trace before it is called a hardware fact).
		b.ioBuffer = 0x30 | pad
		return b.ioBuffer
	case off < 0x1800:
		switch off & 0x03 {
		case 2:
			b.ioBuffer = b.ioBuffer&0xF8 | b.irqMask&0x07
		case 3:
			b.ioBuffer = b.ioBuffer&0xF8 | b.irqLines&0x07
		}
		return b.ioBuffer
	}
	return 0xFF
}

func (b *Bus) writeIO(off uint16, value uint8) {
	switch {
	case off < 0x0400:
		if b.dev.VDC != nil {
			b.dev.VDC.Write(off&0x03, value)
		}
		b.cpuCycle() // extra cycle after a VDC/VCE write, sampled
	case off < 0x0800:
		if b.dev.VCE != nil {
			b.dev.VCE.Write(off&0x07, value)
		}
		b.cpuCycle()
	case off < 0x0C00:
		if b.dev.PSG != nil {
			b.dev.PSG.Write(off&0x0F, value)
		}
		b.ioBuffer = value
	case off < 0x1000:
		b.dev.Timer.write(off&0x01, value)
		b.ioBuffer = value
	case off < 0x1400:
		if b.dev.Pad != nil {
			b.dev.Pad.Write(0, value)
		}
		b.ioBuffer = value
	case off < 0x1800:
		switch off & 0x03 {
		case 2:
			b.irqMask = value & 0x07
		case 3:
			b.irqLines &^= irqTimer
		}
		b.ioBuffer = value
	}
}

// Timer is the HuC6280's 7-bit down counter: every 1024 steps of three
// master clocks (1024 high-speed CPU cycles) it decrements; when it would
// pass zero it reloads and raises the timer IRQ (spec C5).
type Timer struct {
	enabled bool
	reload  uint8
	counter uint8
	scaler  int
}

const timerScaler = 1024

func (t *Timer) reset() {
	*t = Timer{scaler: timerScaler}
}

// tick advances by three-master-clock steps and reports whether the IRQ
// fired.
func (t *Timer) tick(cycles int) bool {
	if !t.enabled {
		return false
	}
	fired := false
	t.scaler -= cycles
	for t.scaler <= 0 {
		t.scaler += timerScaler
		if t.counter == 0 {
			t.counter = t.reload
			fired = true
		} else {
			t.counter--
		}
	}
	return fired
}

func (t *Timer) write(reg uint16, value uint8) {
	if reg == 0 {
		t.reload = value & 0x7F
		return
	}
	enabled := value&0x01 != 0
	if enabled != t.enabled {
		t.enabled = enabled
		t.scaler = timerScaler
		t.counter = t.reload
	}
}

// read returns the counter; in the last five steps before an expiry the
// register briefly reads $7F rather than the reload value (a quirk some
// games depend on, reported by the oracle).
// TimerView exposes the timer for snapshots and oracle tests.
type TimerView struct {
	Enabled         bool
	Reload, Counter uint8
	Scaler          int // 3-master-clock steps left before the counter moves
}

// TimerView returns the timer's registers and prescaler.
func (b *Bus) TimerView() TimerView {
	t := b.dev.Timer
	return TimerView{Enabled: t.enabled, Reload: t.reload, Counter: t.counter, Scaler: t.scaler}
}

// IRQState returns the raw IRQ lines and the $1402 mask.
func (b *Bus) IRQState() (lines, mask uint8) { return b.irqLines, b.irqMask }

func (t *Timer) read() uint8 {
	if t.counter == 0 && t.scaler <= 5 {
		return 0x7F
	}
	return t.counter
}

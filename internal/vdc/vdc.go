// Package vdc is the HuC6270 video display controller: VRAM, the sprite
// attribute table, the vertical/horizontal state machines and a scanline
// renderer. Spec: docs/spec/vdc-vce.md.
//
// 參考行為：Mesen2 PceVdc.cpp @ b9fa69d §vertical mode counters、§in-line event
// offsets、§sprite evaluation、§register semantics（只取行為事實；本檔的排程
// 結構是自己的）。
package vdc

import "github.com/wicanr2/onepce-ai-pacifista/internal/vce"

// IRQLine is the VDC's interrupt output (IRQ1 on the bus).
type IRQLine interface {
	Assert()
	Clear()
}

const (
	clocksPerLine = 1365
	vramWords     = 0x8000
	satWords      = 0x100
	maxLineCells  = 16
	frameLine     = 256 // scanline at which the frame counter advances
)

// Status register bits.
const (
	StatusBusy        uint8 = 0x40
	StatusVBlank      uint8 = 0x20
	StatusVRAMDMADone uint8 = 0x10
	StatusSATBDone    uint8 = 0x08
	StatusRCR         uint8 = 0x04
	StatusOverflow    uint8 = 0x02
	StatusSprite0Hit  uint8 = 0x01
	statusIRQMask           = StatusVBlank | StatusVRAMDMADone | StatusSATBDone | StatusRCR | StatusOverflow | StatusSprite0Hit
)

// Vertical modes (spec §3).
const (
	modeVSW = iota
	modeVDS
	modeVDW
	modeVDE
)

// Horizontal phases (spec §4).
const (
	hSW = iota
	hDS
	hDW
	hDE
)

// Registers is the snapshot view of the 16-bit register file.
type Registers struct {
	Raw      [0x20]uint16
	Selected uint8
	Status   uint8
	Scanline int
	HClock   int
	Frame    uint64
	VMode    int
	RCRCount int
}

// Timing is the latched horizontal/vertical geometry.
type timing struct {
	hsw, hds, hdw, hde uint8
	vsw                uint8
	vds                uint8
	vdw                uint16
	vcr                uint8
	cols, rows         int
	cgMode             bool
	vramMode           uint8
	spriteMode         uint8
}

type spriteCell struct {
	index    int
	x        int
	tile     uint16 // VRAM word address of the cell row (cell*64 + row)
	hflip    bool
	front    bool
	palette  uint8
	loadSP23 bool
}

// Source says who wrote a VRAM word (docs/spec/observe.md O2).
type Source uint8

const (
	ByCPU Source = iota
	ByDMA
	BySATB
)

// VDC is the controller. Zero value is not usable; use New.
type VDC struct {
	vce *vce.VCE
	irq IRQLine

	// OnStartFrame, when set, fires when the scanline counter wraps to 0 —
	// the oracle's "start of frame" event (spec psg.md §3 uses it as the
	// VGM beat; it is not the frame counter's scanline-256 boundary).
	OnStartFrame func()

	// OnVRAMWrite, when set, sees every VRAM word write with its source;
	// SATB transfers report the SAT index instead of a VRAM address.
	OnVRAMWrite func(addr uint16, value uint16, src Source)

	vram [vramWords]uint16
	sat  [satWords]uint16
	reg  [0x20]uint16
	cur  uint8

	openBus uint16
	vwrLow  uint8
	readBuf uint16
	inc     uint16

	// Timing state.
	master    uint64
	hclock    int
	scanline  int
	frame     uint64
	frameDone bool
	vmode     int
	vcounter  int
	rcr       int // RcrCounter: lines since VDW started
	lat       timing
	bxrLatch  uint16
	byrLatch  uint16
	byrPend   bool

	// Per-line schedule (hclock values), computed at line start.
	tLatchY, tLatchX, tIRQ, tRCR, tHDW                int
	doneLatchY, doneLatchX, doneIRQ, doneRCR, doneHDW bool

	// Horizontal phase sequence (spec §4): HSW → HDS → HDW → HDE → HSW…,
	// cut short by the 1365-clock line. hsyncStart is the master clock at
	// which the current HSW began; the 8-dot VRAM block counts from there.
	hMode      int
	hModeEnd   int // hclock at which the phase ends
	hsyncStart uint64

	// VRAM access queue (spec §5.1): one CPU read or write waits here until
	// the VDC has a free VRAM slot; the CPU stalls if it touches the queue
	// again before then.
	pendRead, pendWrite bool
	pendDelay           int    // master clocks before the access may be served
	vwrData             uint16 // word queued by a VWR write
	bgStart, bgEnd      int    // background fetch window of this line (hclock); bgStart<0 = none
	evalStart           int    // sprite evaluation start on this line (hclock); <0 = none
	sprPrev, sprNext    int    // sprite cells fetched for this row / for the next row
	// Stall, when set, advances the whole machine three master clocks; the
	// VDC calls it while the CPU waits on the queue.
	Stall func()

	// Where the last framebuffer sits in the oracle's picture coordinates
	// (docs/spec/framebuffer-parity.md §3): first display dot of the line
	// and the scanline of VDW raster 0.
	fbDot0, fbLine0 int

	needVBlank   bool
	vblankDone   bool
	allowDMA     bool
	overflowLine bool

	status  uint8
	bg, spr bool // effective (latched) enables
	nextBg  bool
	nextSpr bool
	burst   bool

	// Transfers run word by word on VDC ticks (spec §5): SATB first, then
	// the VRAM→VRAM DMA, both only while DMA is allowed.
	satbPending  bool
	satbRunning  bool
	satbOffset   int // next SAT word
	satbCounter  int // master clocks accumulated towards the next word
	dmaRunning   bool
	dmaCounter   int
	dmaReadCycle bool
	dmaBuffer    uint16

	fbW, fbH int
	fb       []uint16
	lineSpr  []spriteCell
}

// New wires the VDC to its colour encoder and interrupt line, in power-on
// state: VSW mode with all-zero geometry until the game programs it.
func New(v *vce.VCE, irq IRQLine) *VDC {
	d := &VDC{vce: v, irq: irq, inc: 1}
	// Power-on geometry the oracle reports before any register is written:
	// 32x32 BAT, HDW=$1F, VDW=239, vertical state machine parked in VDS with
	// a zero counter that only leaves that state when the end of the first
	// frame forces VSW (spec §3).
	d.reg[0x0B] = 0x1F
	d.reg[0x0D] = 239
	d.lat.cols, d.lat.rows = 32, 32
	d.lat.hdw = 0x1F
	d.lat.vdw = 239
	d.vmode = modeVDS
	d.vcounter = 0
	d.bgStart, d.bgEnd, d.evalStart = -1, clocksPerLine, -1
	d.startLine()
	return d
}

// --- snapshots ---

// VRAM returns a copy of the 32K words.
func (d *VDC) VRAM() []uint16 { out := make([]uint16, vramWords); copy(out, d.vram[:]); return out }

// SAT returns a copy of the 256-word sprite attribute table.
func (d *VDC) SAT() []uint16 { out := make([]uint16, satWords); copy(out, d.sat[:]); return out }

// Registers returns the register file and timing counters.
func (d *VDC) Registers() Registers {
	return Registers{Raw: d.reg, Selected: d.cur, Status: d.status, Scanline: d.scanline,
		HClock: d.hclock, Frame: d.frame, VMode: d.vmode, RCRCount: d.rcr}
}

// Frame is the number of completed frames.
func (d *VDC) Frame() uint64 { return d.frame }

// Scanline and HClock locate the current raster position for events.
func (d *VDC) Scanline() int { return d.scanline }
func (d *VDC) HClock() int   { return d.hclock }

// TakeFrameReady reports (and clears) whether a frame boundary passed since
// the last call.
func (d *VDC) TakeFrameReady() bool {
	r := d.frameDone
	d.frameDone = false
	return r
}

// Framebuffer is the last rendered picture: width, height and one 9-bit VCE
// colour per pixel, top-left first. Only the display window is included.
func (d *VDC) Framebuffer() (int, int, []uint16) { return d.fbW, d.fbH, d.fb }

// TransferView is the state of the transfers and the access queue, for
// oracle comparisons and the GUI.
type TransferView struct {
	SATBRunning, SATBPending bool
	SATBOffset, SATBCounter  int
	DMARunning               bool
	DMALen                   uint16
	PendRead, PendWrite      bool
	PendDelay                int
}

// PhaseView is the horizontal phase and fetch-window state of the current
// line, for oracle comparisons.
type PhaseView struct {
	HMode, HModeEnd  int
	EvalStart        int
	HSyncStart       uint64
	BGStart, BGEnd   int
	SprPrev, SprNext int
	SpritesOn, Burst bool
}

// Phase returns the current line's phase/fetch state.
func (d *VDC) Phase() PhaseView {
	return PhaseView{HMode: d.hMode, HModeEnd: d.hModeEnd, HSyncStart: d.hsyncStart, BGStart: d.bgStart, BGEnd: d.bgEnd, EvalStart: d.evalStart,
		SprPrev: d.sprPrev, SprNext: d.sprNext, SpritesOn: d.spr, Burst: d.burst}
}

// Transfers returns the transfer/queue state.
func (d *VDC) Transfers() TransferView {
	return TransferView{SATBRunning: d.satbRunning, SATBPending: d.satbPending, SATBOffset: d.satbOffset,
		SATBCounter: d.satbCounter, DMARunning: d.dmaRunning, DMALen: d.reg[0x12],
		PendRead: d.pendRead, PendWrite: d.pendWrite, PendDelay: d.pendDelay}
}

// DisplayWindow reports where the framebuffer's (0,0) sits in the line/dot
// coordinates the oracle uses: the display-start dot (hclock ÷ VCE divider)
// and the scanline of VDW raster 0 (docs/spec/framebuffer-parity.md §3).
func (d *VDC) DisplayWindow() (dot0, line0 int) { return d.fbDot0, d.fbLine0 }

// --- bus.Device ---

func (d *VDC) Read(port uint16) uint8 {
	switch port & 0x03 {
	case 0:
		s := d.status
		if d.pendRead || d.pendWrite {
			s |= StatusBusy
		}
		d.status &^= statusIRQMask
		d.irq.Clear()
		return s
	case 2:
		if d.pendRead {
			d.waitAccess()
		}
		return uint8(d.readBuf)
	case 3:
		if d.pendRead {
			d.waitAccess()
		}
		v := uint8(d.readBuf >> 8)
		if d.cur == 0x02 {
			d.queueRead()
		}
		return v
	}
	return 0
}

func (d *VDC) Write(port uint16, value uint8) {
	switch port & 0x03 {
	case 0:
		d.cur = value & 0x1F
	case 2:
		d.writeReg(false, value)
	case 3:
		d.writeReg(true, value)
	}
}

func (d *VDC) setReg(i uint8, msb bool, value uint8, mask uint16) {
	if msb {
		d.reg[i] = (d.reg[i]&0x00FF | uint16(value)<<8) & mask
	} else {
		d.reg[i] = (d.reg[i]&0xFF00 | uint16(value)) & mask
	}
}

func (d *VDC) writeReg(msb bool, value uint8) {
	r := d.cur
	switch r {
	case 0x00:
		d.waitAccess()
		d.setReg(r, msb, value, 0xFFFF)
	case 0x01:
		d.waitAccess()
		d.setReg(r, msb, value, 0xFFFF)
		if msb {
			d.queueRead()
		}
	case 0x02:
		d.waitAccess()
		if !msb {
			d.vwrLow = value
			return
		}
		d.vwrData = uint16(value)<<8 | uint16(d.vwrLow)
		d.pendWrite = true
		switch d.div() {
		case 2:
			d.pendDelay = 12
		case 3:
			d.pendDelay = 18
		default:
			d.pendDelay = 21
		}
		// Observers see the write as the CPU's, at the instruction that
		// issued it (spec §5.1); the VRAM word itself lands when served.
		if d.reg[0] < vramWords && d.OnVRAMWrite != nil {
			d.OnVRAMWrite(d.reg[0], d.vwrData, ByCPU)
		}
		if d.Stall == nil {
			d.serveAccess()
		}
	case 0x05:
		d.setReg(r, msb, value, 0xFFFF)
		if msb {
			switch (value >> 3) & 0x03 {
			case 0:
				d.inc = 1
			case 1:
				d.inc = 0x20
			case 2:
				d.inc = 0x40
			case 3:
				d.inc = 0x80
			}
		} else {
			d.nextSpr = value&0x40 != 0
			d.nextBg = value&0x80 != 0
		}
	case 0x06:
		d.setReg(r, msb, value, 0x03FF)
	case 0x07:
		d.setReg(r, msb, value, 0x03FF)
	case 0x08:
		d.setReg(r, msb, value, 0x01FF)
		d.byrPend = true
	case 0x09:
		d.setReg(r, msb, value, 0xFFFF)
	case 0x0A, 0x0B, 0x0C, 0x0D, 0x0E, 0x0F, 0x10, 0x11:
		d.setReg(r, msb, value, 0xFFFF)
	case 0x12:
		d.setReg(r, msb, value, 0xFFFF)
		if msb {
			d.startVRAMDMA()
		}
	case 0x13:
		d.setReg(r, msb, value, 0xFFFF)
		if msb {
			d.satbPending = true
		}
	default:
		d.setReg(r, msb, value, 0xFFFF)
	}
}

func (d *VDC) readVRAM(addr uint16) uint16 {
	if addr < vramWords {
		d.openBus = d.vram[addr]
	}
	return d.openBus
}

func (d *VDC) writeVRAM(addr, value uint16) {
	d.writeVRAMFrom(addr, value, ByCPU)
}

func (d *VDC) writeVRAMFrom(addr, value uint16, src Source) {
	if addr < vramWords {
		d.vram[addr] = value
		if d.OnVRAMWrite != nil {
			d.OnVRAMWrite(addr, value, src)
		}
	}
}

// --- VRAM access queue (spec §5.1) ---

func (d *VDC) queueRead() {
	d.pendRead = true
	switch d.div() {
	case 2:
		d.pendDelay = 15
	default:
		d.pendDelay = 24
	}
	if d.Stall == nil {
		// No machine around us (unit tests): serve at once.
		d.serveAccess()
	}
}

// waitAccess stalls the CPU until the queued access has been served.
func (d *VDC) waitAccess() {
	for d.pendRead || d.pendWrite {
		if d.Stall == nil {
			d.serveAccess()
			return
		}
		d.Stall()
	}
}

// serveAccess performs the queued access now.
func (d *VDC) serveAccess() {
	if d.pendRead {
		d.readBuf = d.readVRAM(d.reg[1])
		d.reg[1] += d.inc
		d.pendRead = false
	} else if d.pendWrite {
		if d.reg[0] < vramWords {
			d.vram[d.reg[0]] = d.vwrData
		}
		d.reg[0] += d.inc
		d.pendWrite = false
	}
	d.pendDelay = 0
}

// processAccess is one VDC tick (three master clocks) of the queue: count
// the delay down, then serve the access at the first free VRAM slot.
func (d *VDC) processAccess() {
	if !d.pendRead && !d.pendWrite {
		return
	}
	if d.pendDelay > 0 {
		d.pendDelay -= 3
		if d.pendDelay > 0 {
			return
		}
	}
	d.pendDelay = 0
	if d.accessBlocked() {
		return
	}
	if d.inHSyncBlock(false) {
		return
	}
	d.serveAccess()
}

// accessBlocked reports whether the VDC is using VRAM itself at this hclock
// (spec §5.1 rule table).
func (d *VDC) accessBlocked() bool {
	div := d.div()
	dotOdd := (d.hclock/div)&1 == 1
	inBg := !d.burst && d.bgStart >= 0 && d.hclock >= d.bgStart && d.hclock < d.bgEnd &&
		d.scanline >= 14 && d.scanline < frameLine
	// Before this line's sprite evaluation the count still belongs to the
	// row being drawn; from the evaluation on it is the next row's.
	cells := d.sprNext
	if d.evalStart < 0 || d.hclock < d.evalStart {
		cells = d.sprPrev
	}
	if d.vmode != modeVDW || d.burst || ((!d.spr || cells == 0) && !inBg) {
		// Blanking, forced blank, or a row with nothing to fetch: one free
		// slot every other dot, none while a DMA runs.
		return d.satbRunning || d.dmaRunning || dotOdd
	}
	if inBg {
		k := (d.hclock-d.bgStart)/div - 1
		if k < 0 {
			return true
		}
		switch d.lat.vramMode {
		case 0:
			return k&1 == 1
		case 3:
			return true
		default:
			return k&7 != 2 && k&7 != 3
		}
	}
	// Sprite pattern fetch for the next row starts when the background
	// fetch ends and takes 4/4/8/16 dots per cell by sprite access mode.
	clocks := d.hclock - d.bgEnd
	if d.hclock <= d.bgEnd {
		clocks = clocksPerLine - d.bgEnd + d.hclock
	}
	per := 4
	switch d.lat.spriteMode {
	case 2:
		per = 8
	case 3:
		per = 16
	}
	if clocks/div < cells*per {
		return true
	}
	return dotOdd
}

// spriteCellCount is the number of 16-pixel sprite cells the VDC fetches
// for a row, capped at the per-line limit (same walk as evalSprites).
func (d *VDC) spriteCellCount(row int) int {
	cells := 0
	for i := 0; i < 64; i++ {
		y := int(d.sat[i*4]&0x3FF) - 64
		if row < y {
			continue
		}
		height := 16
		switch (d.sat[i*4+3] >> 12) & 0x03 {
		case 1:
			height = 32
		case 2, 3:
			height = 64
		}
		if row >= y+height {
			continue
		}
		width := 1
		if d.sat[i*4+3]&0x100 != 0 {
			width = 2
		}
		for x := 0; x < width; x++ {
			if cells >= maxLineCells {
				return cells
			}
			cells++
		}
	}
	return cells
}

// --- register accessors ---

func (d *VDC) cr() uint16     { return d.reg[0x05] }
func (d *VDC) rcrReg() int    { return int(d.reg[0x06] & 0x3FF) }
func (d *VDC) dcr() uint16    { return d.reg[0x0F] }
func (d *VDC) lines() int     { return d.vce.Lines() }
func (d *VDC) div() int       { return d.vce.ClockDivider() }
func (d *VDC) dots(n int) int { return n * d.div() }

func (d *VDC) latchVertical() {
	d.lat.vsw = uint8(d.reg[0x0C] & 0x1F)
	d.lat.vds = uint8(d.reg[0x0C] >> 8)
	d.lat.vdw = d.reg[0x0D] & 0x1FF
	d.lat.vcr = uint8(d.reg[0x0E])
	mwr := d.reg[0x09]
	switch (mwr >> 4) & 0x03 {
	case 0:
		d.lat.cols = 32
	case 1:
		d.lat.cols = 64
	default:
		d.lat.cols = 128
	}
	if mwr&0x40 != 0 {
		d.lat.rows = 64
	} else {
		d.lat.rows = 32
	}
	d.lat.vramMode = uint8(mwr & 0x03)
	d.lat.spriteMode = uint8((mwr >> 2) & 0x03)
	d.lat.cgMode = mwr&0x80 != 0
}

func (d *VDC) latchHorizontal() {
	d.lat.hsw = uint8(d.reg[0x0A] & 0x1F)
	d.lat.hds = uint8((d.reg[0x0A] >> 8) & 0x7F)
	d.lat.hdw = uint8(d.reg[0x0B] & 0x7F)
	d.lat.hde = uint8((d.reg[0x0B] >> 8) & 0x7F)
}

// --- vertical state machine (spec §3) ---

func (d *VDC) setVMode(m int) {
	d.vmode = m
	switch m {
	case modeVSW:
		d.latchVertical()
		d.vcounter = int(d.lat.vsw) + 1
	case modeVDS:
		d.vcounter = int(d.lat.vds) + 2
	case modeVDW:
		d.allowDMA = false
		d.rcr = 0
		d.vcounter = int(d.lat.vdw) + 1
	case modeVDE:
		d.vcounter = int(d.lat.vcr)
	}
}

func (d *VDC) clockVCounter() {
	d.vcounter--
	if d.vcounter == 0 {
		d.setVMode((d.vmode + 1) % 4)
	}
}

func (d *VDC) incrementRCR() {
	d.rcr++
	d.clockVCounter()
	if d.vmode == modeVDE && d.rcr == int(d.lat.vdw)+1 {
		d.needVBlank = true
		d.vblankDone = true
	}
	if d.cr()&0x04 != 0 && d.rcr == d.rcrReg()-0x40 {
		d.status |= StatusRCR
		d.irq.Assert()
	}
}

func (d *VDC) triggerVBlank() {
	if d.cr()&0x08 != 0 {
		d.status |= StatusVBlank
		d.irq.Assert()
	}
	d.needVBlank = false
	d.allowDMA = true
	if d.satbPending || d.dcr()&0x10 != 0 {
		d.satbPending = false
		d.startSATB()
	}
}

func (d *VDC) hdsIRQTrigger() {
	if d.needVBlank {
		d.triggerVBlank()
	}
	if d.overflowLine && d.cr()&0x02 != 0 {
		d.status |= StatusOverflow
		d.irq.Assert()
	}
	d.overflowLine = false
}

func (d *VDC) latchScrollY() {
	if d.rcr == 0 {
		d.byrLatch = d.reg[0x08] & 0x1FF
	} else {
		if d.byrPend {
			d.byrLatch = d.reg[0x08] & 0x1FF
			d.byrPend = false
		}
		d.byrLatch = (d.byrLatch + 1) & 0x1FF
	}
	if !d.burst {
		d.bg, d.spr = d.nextBg, d.nextSpr
	}
}

// --- per-line schedule (spec §4) ---

func (d *VDC) startLine() {
	d.hclock = 0
	d.latchHorizontal()
	hswEnd := d.dots(24)
	if d.div() == 3 {
		hswEnd = d.dots(32)
	}
	// The line restart forces HSW; if the VDC was already in HSW (HDE ended
	// early) the sync start is not refreshed (Mesen2 ProcessEndOfScanline).
	if d.hMode != hSW {
		d.hMode = hSW
		d.hsyncStart = d.master
	}
	d.hModeEnd = hswEnd
	displayStart := hswEnd + d.dots((int(d.lat.hds)+1)*8)
	d.tHDW = displayStart
	d.tRCR = displayStart + d.dots((int(d.lat.hdw)-1)*8+2)
	lastLine := d.rcr == d.lines()-1
	if d.vmode == modeVDW || lastLine {
		d.tLatchY = displayStart - d.dots(34)
		d.tLatchX = d.tLatchY + d.dots(2)
		d.tIRQ = d.tLatchX + d.dots(6)
	} else {
		d.tLatchY, d.tLatchX = -1, -1
		d.tIRQ = displayStart - d.dots(25)
	}
	// Background fetch window and sprite cell counts for the access queue
	// (spec §5.1): fetching starts 16 dots before the display and runs 16
	// dots past it; the next row's sprites are evaluated on this line.
	d.sprPrev = d.sprNext
	d.bgStart, d.evalStart = -1, -1
	if displayStart-d.dots(24) < clocksPerLine && (d.vmode == modeVDW || lastLine) {
		// The row evaluated is the next one, wrapping on the last line of
		// the frame (Mesen2: spriteRow = (RcrCounter + 1) % scanlineCount).
		d.evalStart = displayStart - d.dots(16)
		d.sprNext = d.spriteCellCount((d.rcr + 1) % d.lines())
		if d.vmode == modeVDW {
			d.bgStart = displayStart - d.dots(16)
			d.bgEnd = d.bgStart + d.dots((int(d.lat.hdw)+1)*8+16)
			if d.bgEnd > clocksPerLine {
				d.bgEnd = clocksPerLine
			}
		}
	} else {
		d.sprNext = 0
	}
	if displayStart-d.dots(24) >= clocksPerLine {
		// Display would start past the end of the line: nothing fires.
		d.tLatchY, d.tLatchX, d.tIRQ, d.tHDW = -1, -1, -1, -1
	}
	d.doneLatchY, d.doneLatchX, d.doneIRQ, d.doneRCR, d.doneHDW = false, false, false, false, false
	d.runEvents()
}

// stepHMode advances the horizontal phase when its end has been reached.
func (d *VDC) stepHMode() {
	for d.hclock >= d.hModeEnd && d.hModeEnd < clocksPerLine {
		switch d.hMode {
		case hSW:
			d.hMode = hDS
			d.hModeEnd += d.dots((int(d.lat.hds) + 1) * 8)
		case hDS:
			d.hMode = hDW
			d.hModeEnd += d.dots((int(d.lat.hdw) + 1) * 8)
		case hDW:
			d.hMode = hDE
			d.hModeEnd += d.dots((int(d.lat.hde) + 1) * 8)
		default:
			d.hMode = hSW
			// The oracle stamps the sync start with its tick clock, which is
			// the phase end rounded up to the next multiple of three.
			lineStart := d.master - uint64(d.hclock)
			d.hsyncStart = lineStart + uint64((d.hModeEnd+2)/3*3)
			d.hModeEnd += d.dots((int(d.lat.hsw) + 1) * 8)
		}
	}
}

// inHSyncBlock reports whether a VRAM slot is blocked by the first 8 dots
// of horizontal sync; the oracle measures "tick clock − sync start", which
// is our master + 3, with < for the access queue and <= for transfers.
func (d *VDC) inHSyncBlock(transfer bool) bool {
	if d.hMode != hSW {
		return false
	}
	since := int(d.master + 3 - d.hsyncStart)
	if transfer {
		return since <= d.dots(8)
	}
	return since < d.dots(8)
}

// runEvents fires every scheduled event whose time has been reached.
func (d *VDC) runEvents() {
	d.stepHMode()
	if !d.doneLatchY && d.tLatchY >= 0 && d.hclock >= d.tLatchY {
		d.doneLatchY = true
		d.latchScrollY()
	}
	if !d.doneLatchX && d.tLatchX >= 0 && d.hclock >= d.tLatchX {
		d.doneLatchX = true
		d.bxrLatch = d.reg[0x07] & 0x3FF
	}
	if !d.doneIRQ && d.tIRQ >= 0 && d.hclock >= d.tIRQ {
		d.doneIRQ = true
		d.hdsIRQTrigger()
	}
	if !d.doneHDW && d.tHDW >= 0 && d.hclock >= d.tHDW {
		d.doneHDW = true
		if d.vmode == modeVDW {
			if d.rcr == 0 {
				d.fbDot0, d.fbLine0 = d.tHDW/d.div(), d.scanline
			}
			d.renderLine(d.rcr)
		}
	}
	if !d.doneRCR && d.hclock >= d.tRCR {
		d.doneRCR = true
		d.incrementRCR()
	}
}

// nextEventClock is the smallest pending event time after hclock, or the
// end of the line.
func (d *VDC) nextEventClock() int {
	next := clocksPerLine
	consider := func(done bool, t int) {
		if !done && t > d.hclock && t < next {
			next = t
		}
	}
	consider(d.doneLatchY, d.tLatchY)
	consider(d.doneLatchX, d.tLatchX)
	consider(d.doneIRQ, d.tIRQ)
	consider(d.doneHDW, d.tHDW)
	consider(d.doneRCR, d.tRCR)
	if d.hModeEnd > d.hclock && d.hModeEnd < next {
		next = d.hModeEnd
	}
	return next
}

func (d *VDC) endLine() {
	d.scanline++
	if d.scanline == frameLine {
		// The frame boundary the oracle reports (frame counter, input poll,
		// end-of-frame callbacks) is scanline 256, not the wrap to 0.
		d.frame++
		d.frameDone = true
	}
	if d.scanline >= d.lines() {
		d.scanline = 0
		d.vblankDone = false
		d.burst = !d.nextBg && !d.nextSpr
		d.bg, d.spr = d.nextBg, d.nextSpr
	}
	if !d.doneRCR {
		d.doneRCR = true
		d.incrementRCR()
	}
	if d.scanline == 0 && d.OnStartFrame != nil {
		d.OnStartFrame()
	}
	if d.scanline == d.lines()-3 {
		d.setVMode(modeVSW)
	} else if d.scanline == d.lines()-2 && !d.vblankDone {
		d.needVBlank = true
	}
	d.startLine()
}

// Advance runs the VDC for n master clocks.
func (d *VDC) Advance(n uint64) {
	target := d.master + n
	for d.master < target {
		step := int(target - d.master)
		if limit := d.nextEventClock() - d.hclock; limit < step {
			step = limit
		}
		if d.satbRunning || d.dmaRunning || d.pendRead || d.pendWrite {
			// Transfers and the access queue move on VDC ticks of three
			// master clocks, in that order (Mesen2 PceVdc::Exec).
			if phase := int(d.master % 3); phase == 0 {
				d.transferTick()
				d.processAccess()
				if step > 3 {
					step = 3
				}
			} else if step > 3-phase {
				step = 3 - phase
			}
		}
		d.hclock += step
		d.master += uint64(step)
		d.runEvents()
		if d.hclock >= clocksPerLine {
			d.endLine()
		}
	}
}

// --- DMA (spec §5) ---

func (d *VDC) startSATB() {
	d.satbRunning = true
	d.satbOffset, d.satbCounter = 0, 0
}

// startVRAMDMA arms the VRAM→VRAM transfer; it moves only while DMA is
// allowed (blanking or forced blank) and after any SATB transfer.
func (d *VDC) startVRAMDMA() {
	d.dmaRunning = true
	d.dmaReadCycle = true
	d.dmaCounter = 0
}

// dmaAllowed: no transfers during the picture, nor in the first 8 dots of
// horizontal sync (Mesen2 PceVdc::IsDmaAllowed).
func (d *VDC) dmaAllowed() bool {
	return (d.allowDMA || d.burst) && !d.inHSyncBlock(true)
}

// transferTick is one VDC tick of the running transfer: one SAT word per
// 4 dots, or one VRAM DMA read/write per 2 dots.
func (d *VDC) transferTick() {
	if !d.dmaAllowed() {
		return
	}
	div := d.div()
	if d.satbRunning {
		d.satbCounter += 3
		if d.satbCounter/div >= 4 {
			d.satbCounter -= 4 * div
			i := d.satbOffset
			d.sat[i] = d.readVRAM(d.reg[0x13] + uint16(i))
			if d.OnVRAMWrite != nil {
				d.OnVRAMWrite(uint16(i), d.sat[i], BySATB)
			}
			d.satbOffset++
			if d.satbOffset == satWords {
				d.satbRunning = false
				if d.dcr()&0x01 != 0 {
					d.status |= StatusSATBDone
					d.irq.Assert()
				}
			}
		}
		return
	}
	if !d.dmaRunning {
		return
	}
	d.dmaCounter += 3
	per := div * 2
	for d.dmaCounter >= per {
		if d.dmaReadCycle {
			d.dmaBuffer = d.readVRAM(d.reg[0x10])
			d.dmaReadCycle = false
		} else {
			d.reg[0x12]--
			d.writeVRAMFrom(d.reg[0x11], d.dmaBuffer, ByDMA)
			if d.dcr()&0x04 != 0 {
				d.reg[0x10]--
			} else {
				d.reg[0x10]++
			}
			if d.dcr()&0x08 != 0 {
				d.reg[0x11]--
			} else {
				d.reg[0x11]++
			}
			d.dmaReadCycle = true
			if d.reg[0x12] == 0xFFFF {
				d.dmaRunning = false
				d.dmaCounter = 0
				if d.dcr()&0x02 != 0 {
					d.status |= StatusVRAMDMADone
					d.irq.Assert()
				}
				break
			}
		}
		d.dmaCounter -= per
	}
}

// --- rendering (spec §6) ---

func (d *VDC) ensureFramebuffer() {
	w := (int(d.lat.hdw) + 1) * 8
	h := int(d.lat.vdw) + 1
	if w != d.fbW || h != d.fbH || d.fb == nil {
		d.fbW, d.fbH = w, h
		d.fb = make([]uint16, w*h)
	}
}

func tilePixel(p01, p23 uint16, x int) uint8 {
	shift := uint(7 - x)
	return uint8((p01>>shift)&1) | uint8((p01>>(8+shift))&1)<<1 |
		uint8((p23>>shift)&1)<<2 | uint8((p23>>(8+shift))&1)<<3
}

func (d *VDC) evalSprites(row int) {
	d.lineSpr = d.lineSpr[:0]
	cells := 0
	for i := 0; i < 64; i++ {
		y := int(d.sat[i*4]&0x3FF) - 64
		if row < y {
			continue
		}
		flags := d.sat[i*4+3]
		height := 16
		switch (flags >> 12) & 0x03 {
		case 1:
			height = 32
		case 2, 3:
			height = 64
		}
		if row >= y+height {
			continue
		}
		spriteRow := row - y
		vflip := flags&0x8000 != 0
		hflip := flags&0x0800 != 0
		var yOff, rowOff int
		if vflip {
			yOff = (height - spriteRow - 1) & 0x0F
			rowOff = (height - spriteRow - 1) >> 4
		} else {
			yOff = spriteRow & 0x0F
			rowOff = spriteRow >> 4
		}
		tile := (d.sat[i*4+2] & 0x7FF) >> 1
		width := 16
		if flags&0x100 != 0 {
			width = 32
			tile &^= 0x01
		}
		switch height {
		case 32:
			tile &^= 0x02
		case 64:
			tile &^= 0x06
		}
		tileY := tile | uint16(rowOff)<<1
		for x := 0; x < width; x += 16 {
			if cells >= maxLineCells {
				d.overflowLine = true
				break
			}
			col := x >> 4
			if hflip {
				col = (width - x - 1) >> 4
			}
			d.lineSpr = append(d.lineSpr, spriteCell{
				index:    i,
				x:        int(d.sat[i*4+1]&0x3FF) - 32 + x,
				tile:     (tileY|uint16(col))*64 + uint16(yOff),
				hflip:    hflip,
				front:    flags&0x80 != 0,
				palette:  uint8(flags & 0x0F),
				loadSP23: d.sat[i*4+2]&0x01 != 0,
			})
			cells++
		}
	}
}

func (d *VDC) spritePixel(c *spriteCell, x int) uint8 {
	off := x - c.x
	if off < 0 || off >= 16 {
		return 0
	}
	shift := uint(off)
	if !c.hflip {
		shift = uint(15 - off)
	}
	a := c.tile
	if d.lat.spriteMode == 1 {
		// 2bpp sprites: planes 0/1 or 2/3 selected per sprite.
		if c.loadSP23 {
			a += 32
		}
		return uint8((d.readVRAM(a)>>shift)&1) | uint8((d.readVRAM(a+16)>>shift)&1)<<1
	}
	return uint8((d.readVRAM(a)>>shift)&1) | uint8((d.readVRAM(a+16)>>shift)&1)<<1 |
		uint8((d.readVRAM(a+32)>>shift)&1)<<2 | uint8((d.readVRAM(a+48)>>shift)&1)<<3
}

func (d *VDC) renderLine(row int) {
	d.ensureFramebuffer()
	if row < 0 || row >= d.fbH {
		return
	}
	out := d.fb[row*d.fbW : (row+1)*d.fbW]
	backdrop := d.vce.Color(0)
	if !d.bg && !d.spr {
		for x := range out {
			out[x] = backdrop
		}
		return
	}
	d.evalSprites(row)
	cols, rows := d.lat.cols, d.lat.rows
	bgRow := int(d.byrLatch) & (rows*8 - 1)
	scrollX := int(d.bxrLatch)
	batBase := uint16((bgRow >> 3) * cols)
	for x := 0; x < d.fbW; x++ {
		color := backdrop
		var bgColor uint8
		if d.bg {
			sx := scrollX + x
			entry := d.vram[batBase+uint16((sx>>3)&(cols-1))]
			tileAddr := (entry & 0x0FFF) * 16
			p01 := d.readVRAM(tileAddr + uint16(bgRow&7))
			p23 := d.readVRAM(tileAddr + uint16(bgRow&7) + 8)
			if d.lat.vramMode == 3 {
				if d.lat.cgMode {
					p01, p23 = p23, 0
				} else {
					p23 = 0
				}
			}
			bgColor = tilePixel(p01, p23, sx&7)
			if bgColor != 0 {
				color = d.vce.Color(uint16(entry>>12)*16 + uint16(bgColor))
			}
		}
		if d.spr {
			drawn := false
			sprite0 := false
			for i := range d.lineSpr {
				c := &d.lineSpr[i]
				sc := d.spritePixel(c, x)
				if sc == 0 {
					continue
				}
				if !drawn {
					if bgColor == 0 || c.front {
						color = d.vce.Color(256 + uint16(c.palette)*16 + uint16(sc))
					}
					drawn = true
					if c.index == 0 {
						sprite0 = true
						continue
					}
					break
				}
				if sprite0 && c.index != 0 && d.cr()&0x01 != 0 {
					d.status |= StatusSprite0Hit
					d.irq.Assert()
				}
				break
			}
		}
		out[x] = color
	}
}

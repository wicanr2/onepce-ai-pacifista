// Package vdc is the HuC6270 video display controller: VRAM, the sprite
// attribute table, the vertical/horizontal state machines and a scanline
// renderer. Spec: docs/spec/vdc-vce.md.
//
// 參考行為：Mesen2 PceVdc.cpp @ b9fa69d §vertical mode counters、§in-line event
// offsets、§sprite evaluation、§register semantics（只取行為事實；本檔的排程
// 結構是自己的）。
package vdc

import "github.com/wicanr2/onepce-ai-remake/internal/vce"

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

// VDC is the controller. Zero value is not usable; use New.
type VDC struct {
	vce *vce.VCE
	irq IRQLine

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

	needVBlank   bool
	vblankDone   bool
	allowDMA     bool
	overflowLine bool

	status  uint8
	bg, spr bool // effective (latched) enables
	nextBg  bool
	nextSpr bool
	burst   bool

	satbPending bool
	satbRunning bool
	satbDoneAt  uint64
	dmaPending  bool
	dmaRunning  bool
	dmaDoneAt   uint64

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

// --- bus.Device ---

func (d *VDC) Read(port uint16) uint8 {
	switch port & 0x03 {
	case 0:
		s := d.status
		d.status &^= statusIRQMask
		d.irq.Clear()
		return s
	case 2:
		return uint8(d.readBuf)
	case 3:
		v := uint8(d.readBuf >> 8)
		if d.cur == 0x02 {
			d.reg[1] += d.inc
			d.readBuf = d.readVRAM(d.reg[1])
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
		d.setReg(r, msb, value, 0xFFFF)
	case 0x01:
		d.setReg(r, msb, value, 0xFFFF)
		if msb {
			d.readBuf = d.readVRAM(d.reg[1])
		}
	case 0x02:
		if !msb {
			d.vwrLow = value
			return
		}
		d.writeVRAM(d.reg[0], uint16(value)<<8|uint16(d.vwrLow))
		d.reg[0] += d.inc
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
	if addr < vramWords {
		d.vram[addr] = value
	}
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
	if d.dmaPending {
		d.dmaPending = false
		d.startVRAMDMA()
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
	if displayStart-d.dots(24) >= clocksPerLine {
		// Display would start past the end of the line: nothing fires.
		d.tLatchY, d.tLatchX, d.tIRQ, d.tHDW = -1, -1, -1, -1
	}
	d.doneLatchY, d.doneLatchX, d.doneIRQ, d.doneRCR, d.doneHDW = false, false, false, false, false
	d.runEvents()
}

// runEvents fires every scheduled event whose time has been reached.
func (d *VDC) runEvents() {
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
		d.finishTransfers()
		step := int(target - d.master)
		if limit := d.nextEventClock() - d.hclock; limit < step {
			step = limit
		}
		d.hclock += step
		d.master += uint64(step)
		d.runEvents()
		if d.hclock >= clocksPerLine {
			d.endLine()
		}
	}
	d.finishTransfers()
}

// --- DMA (spec §5) ---

func (d *VDC) startSATB() {
	src := d.reg[0x13]
	for i := 0; i < satWords; i++ {
		d.sat[i] = d.readVRAM(src + uint16(i))
	}
	d.satbRunning = true
	d.satbDoneAt = d.master + uint64(d.dots(4*satWords))
}

func (d *VDC) startVRAMDMA() {
	if !d.allowDMA && !d.burst {
		d.dmaPending = true
		return
	}
	src, dst := d.reg[0x10], d.reg[0x11]
	n := int(d.reg[0x12]) + 1
	for i := 0; i < n; i++ {
		d.writeVRAM(dst, d.readVRAM(src))
		if d.dcr()&0x04 != 0 {
			src--
		} else {
			src++
		}
		if d.dcr()&0x08 != 0 {
			dst--
		} else {
			dst++
		}
	}
	d.reg[0x10], d.reg[0x11] = src, dst
	d.reg[0x12] = 0xFFFF
	d.dmaRunning = true
	d.dmaDoneAt = d.master + uint64(d.dots(4*n))
}

func (d *VDC) finishTransfers() {
	if d.satbRunning && d.master >= d.satbDoneAt {
		d.satbRunning = false
		if d.dcr()&0x01 != 0 {
			d.status |= StatusSATBDone
			d.irq.Assert()
		}
	}
	if d.dmaRunning && d.master >= d.dmaDoneAt {
		d.dmaRunning = false
		if d.dcr()&0x02 != 0 {
			d.status |= StatusVRAMDMADone
			d.irq.Assert()
		}
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

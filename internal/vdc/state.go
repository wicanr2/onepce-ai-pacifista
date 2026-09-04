package vdc

// Timing is the exported copy of the latched geometry.
type Timing struct {
	HSW, HDS, HDW, HDE uint8
	VSW                uint8
	VDS                uint8
	VDW                uint16
	VCR                uint8
	Cols, Rows         int
	CGMode             bool
	VRAMMode           uint8
	SpriteMode         uint8
}

// State is the serialisable controller (docs/spec/state.md S4): everything
// that influences future output, including in-line schedule and DMA timing.
type State struct {
	VRAM    [vramWords]uint16
	SAT     [satWords]uint16
	Reg     [0x20]uint16
	Cur     uint8
	OpenBus uint16
	VWRLow  uint8
	ReadBuf uint16
	Inc     uint16

	Master    uint64
	HClock    int
	Scanline  int
	Frame     uint64
	FrameDone bool
	VMode     int
	VCounter  int
	RCR       int
	Lat       Timing
	BXRLatch  uint16
	BYRLatch  uint16
	BYRPend   bool

	TLatchY, TLatchX, TIRQ, TRCR, THDW                int
	DoneLatchY, DoneLatchX, DoneIRQ, DoneRCR, DoneHDW bool

	NeedVBlank, VBlankDone, AllowDMA, OverflowLine bool
	Status                                         uint8
	BG, SPR, NextBG, NextSPR, Burst                bool

	SATBPending, SATBRunning bool
	SATBDoneAt               uint64
	DMAPending, DMARunning   bool
	DMADoneAt                uint64

	FBW, FBH int
	FB       []uint16
}

// Save copies the controller state out.
func (d *VDC) Save() State {
	s := State{
		VRAM: d.vram, SAT: d.sat, Reg: d.reg, Cur: d.cur, OpenBus: d.openBus, VWRLow: d.vwrLow,
		ReadBuf: d.readBuf, Inc: d.inc,
		Master: d.master, HClock: d.hclock, Scanline: d.scanline, Frame: d.frame, FrameDone: d.frameDone,
		VMode: d.vmode, VCounter: d.vcounter, RCR: d.rcr,
		Lat: Timing{HSW: d.lat.hsw, HDS: d.lat.hds, HDW: d.lat.hdw, HDE: d.lat.hde, VSW: d.lat.vsw,
			VDS: d.lat.vds, VDW: d.lat.vdw, VCR: d.lat.vcr, Cols: d.lat.cols, Rows: d.lat.rows,
			CGMode: d.lat.cgMode, VRAMMode: d.lat.vramMode, SpriteMode: d.lat.spriteMode},
		BXRLatch: d.bxrLatch, BYRLatch: d.byrLatch, BYRPend: d.byrPend,
		TLatchY: d.tLatchY, TLatchX: d.tLatchX, TIRQ: d.tIRQ, TRCR: d.tRCR, THDW: d.tHDW,
		DoneLatchY: d.doneLatchY, DoneLatchX: d.doneLatchX, DoneIRQ: d.doneIRQ, DoneRCR: d.doneRCR, DoneHDW: d.doneHDW,
		NeedVBlank: d.needVBlank, VBlankDone: d.vblankDone, AllowDMA: d.allowDMA, OverflowLine: d.overflowLine,
		Status: d.status, BG: d.bg, SPR: d.spr, NextBG: d.nextBg, NextSPR: d.nextSpr, Burst: d.burst,
		SATBPending: d.satbPending, SATBRunning: d.satbRunning, SATBDoneAt: d.satbDoneAt,
		DMAPending: d.dmaPending, DMARunning: d.dmaRunning, DMADoneAt: d.dmaDoneAt,
		FBW: d.fbW, FBH: d.fbH,
	}
	s.FB = append([]uint16(nil), d.fb...)
	return s
}

// Restore loads the controller state; the VCE and IRQ bindings and the
// write hook stay as they are.
func (d *VDC) Restore(s State) {
	d.vram, d.sat, d.reg, d.cur, d.openBus, d.vwrLow = s.VRAM, s.SAT, s.Reg, s.Cur, s.OpenBus, s.VWRLow
	d.readBuf, d.inc = s.ReadBuf, s.Inc
	d.master, d.hclock, d.scanline, d.frame, d.frameDone = s.Master, s.HClock, s.Scanline, s.Frame, s.FrameDone
	d.vmode, d.vcounter, d.rcr = s.VMode, s.VCounter, s.RCR
	d.lat = timing{hsw: s.Lat.HSW, hds: s.Lat.HDS, hdw: s.Lat.HDW, hde: s.Lat.HDE, vsw: s.Lat.VSW,
		vds: s.Lat.VDS, vdw: s.Lat.VDW, vcr: s.Lat.VCR, cols: s.Lat.Cols, rows: s.Lat.Rows,
		cgMode: s.Lat.CGMode, vramMode: s.Lat.VRAMMode, spriteMode: s.Lat.SpriteMode}
	d.bxrLatch, d.byrLatch, d.byrPend = s.BXRLatch, s.BYRLatch, s.BYRPend
	d.tLatchY, d.tLatchX, d.tIRQ, d.tRCR, d.tHDW = s.TLatchY, s.TLatchX, s.TIRQ, s.TRCR, s.THDW
	d.doneLatchY, d.doneLatchX, d.doneIRQ, d.doneRCR, d.doneHDW = s.DoneLatchY, s.DoneLatchX, s.DoneIRQ, s.DoneRCR, s.DoneHDW
	d.needVBlank, d.vblankDone, d.allowDMA, d.overflowLine = s.NeedVBlank, s.VBlankDone, s.AllowDMA, s.OverflowLine
	d.status, d.bg, d.spr, d.nextBg, d.nextSpr, d.burst = s.Status, s.BG, s.SPR, s.NextBG, s.NextSPR, s.Burst
	d.satbPending, d.satbRunning, d.satbDoneAt = s.SATBPending, s.SATBRunning, s.SATBDoneAt
	d.dmaPending, d.dmaRunning, d.dmaDoneAt = s.DMAPending, s.DMARunning, s.DMADoneAt
	d.fbW, d.fbH = s.FBW, s.FBH
	d.fb = append([]uint16(nil), s.FB...)
	d.lineSpr = d.lineSpr[:0]
}

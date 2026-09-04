package vdc

import (
	"testing"

	"github.com/wicanr2/onepce-ai-remake/internal/vce"
)

type fakeIRQ struct{ asserted bool }

func (f *fakeIRQ) Assert() { f.asserted = true }
func (f *fakeIRQ) Clear()  { f.asserted = false }

func newVDC() (*VDC, *fakeIRQ) {
	irq := &fakeIRQ{}
	return New(vce.New(), irq), irq
}

func (d *VDC) selectReg(r uint8)     { d.Write(0, r) }
func (d *VDC) writeWord(v uint16)    { d.Write(2, uint8(v)); d.Write(3, uint8(v>>8)) }
func (d *VDC) readWord() uint16      { return uint16(d.Read(2)) | uint16(d.Read(3))<<8 }
func (d *VDC) set(r uint8, v uint16) { d.selectReg(r); d.writeWord(v) }

// H-029: MAWR then VWR words, address advancing after every high byte.
func TestVRAMWriteSequenceAdvancesTheAddress(t *testing.T) {
	d, _ := newVDC()
	d.set(0x00, 0x1234)
	d.selectReg(0x02)
	d.writeWord(0xBEEF)
	d.writeWord(0xCAFE)
	if d.vram[0x1234] != 0xBEEF || d.vram[0x1235] != 0xCAFE {
		t.Fatalf("vram[1234..]=%04X %04X", d.vram[0x1234], d.vram[0x1235])
	}
	// Increment 32 via CR bits 11–12.
	d.set(0x05, 0x0800)
	d.set(0x00, 0x2000)
	d.selectReg(0x02)
	d.writeWord(1)
	d.writeWord(2)
	if d.vram[0x2000] != 1 || d.vram[0x2020] != 2 {
		t.Fatal("CR increment 32 not applied")
	}
}

func TestVRAMReadPrefetchesAndAdvancesOnlyWhenVRRSelected(t *testing.T) {
	d, _ := newVDC()
	d.vram[0x0100], d.vram[0x0101] = 0x1111, 0x2222
	d.set(0x01, 0x0100) // MARR: prefetch
	d.selectReg(0x02)
	if got := d.readWord(); got != 0x1111 {
		t.Fatalf("first read %04X", got)
	}
	if got := d.readWord(); got != 0x2222 {
		t.Fatalf("second read %04X (address must advance after the high byte)", got)
	}
	// MARR advances when a prefetch is served: once for the MARR write and
	// once per high-byte read with VRR selected (spec §5.1 Q1), so two reads
	// leave it at $0103. Reads with another register selected do not queue.
	d.selectReg(0x05)
	d.readWord()
	if d.reg[1] != 0x0103 {
		t.Fatalf("MARR advanced while another register was selected: %04X", d.reg[1])
	}
}

func TestStatusReadClearsFlagsAndIRQ(t *testing.T) {
	d, irq := newVDC()
	d.set(0x05, 0x0008) // enable VBlank IRQ
	d.needVBlank = true
	d.hdsIRQTrigger()
	if d.status&StatusVBlank == 0 || !irq.asserted {
		t.Fatal("VBlank flag / IRQ not raised")
	}
	if got := d.Read(0); got&StatusVBlank == 0 {
		t.Fatalf("status read %02X", got)
	}
	if d.status&StatusVBlank != 0 || irq.asserted {
		t.Fatal("status read must clear the flags and drop IRQ1")
	}
}

// Vertical state machine: VSW+1, VDS+2, VDW+1, VCR lines (spec §3).
func TestVerticalModeSequence(t *testing.T) {
	d, _ := newVDC()
	d.set(0x0C, 0x0F02) // VSW=2, VDS=15
	d.set(0x0D, 239)    // VDW
	d.set(0x0E, 4)      // VCR
	d.setVMode(modeVSW)
	seq := map[int]int{}
	mode := d.vmode
	lines := 0
	for i := 0; i < 300 && lines < 4; i++ {
		d.incrementRCR()
		if d.vmode != mode {
			seq[mode] = i + 1 - sum(seq)
			mode = d.vmode
			lines++
		}
	}
	if seq[modeVSW] != 3 || seq[modeVDS] != 17 || seq[modeVDW] != 240 || seq[modeVDE] != 4 {
		t.Fatalf("mode lengths %v, want VSW 3 VDS 17 VDW 240 VDE 4", seq)
	}
}

func sum(m map[int]int) int {
	s := 0
	for _, v := range m {
		s += v
	}
	return s
}

func TestFrameAdvancesThroughAllScanlines(t *testing.T) {
	d, _ := newVDC()
	d.Advance(1365 * 262)
	if d.Frame() != 1 || d.scanline != 0 {
		t.Fatalf("frame=%d scanline=%d after one frame of clocks", d.Frame(), d.scanline)
	}
	d.TakeFrameReady()
	d.Advance(1365 * 255)
	if d.TakeFrameReady() {
		t.Fatal("frame must not be reported before scanline 256")
	}
	d.Advance(1365)
	if d.Frame() != 2 {
		t.Fatalf("frame=%d, want 2 at scanline 256", d.Frame())
	}
	if !d.TakeFrameReady() || d.TakeFrameReady() {
		t.Fatal("frame-ready flag must be reported exactly once per frame")
	}
}

func TestSATBTransferCopiesAtVBlank(t *testing.T) {
	d, _ := newVDC()
	for i := 0; i < satWords; i++ {
		d.vram[0x7F00+i] = uint16(i)
	}
	d.set(0x13, 0x7F00)
	d.set(0x0F, 0x0001) // SATB completion IRQ
	if d.sat[1] != 0 {
		t.Fatal("SATB must wait for VBlank")
	}
	d.needVBlank = true
	d.hdsIRQTrigger()
	if !d.satbRunning || d.sat[255] != 0 {
		t.Fatal("SATB must start at VBlank and copy word by word, not at once")
	}
	// 256 words at 4 dots each, plus the 8-dot pause at each sync start.
	d.Advance(uint64(d.dots(4*satWords + 16*8)))
	if d.sat[1] != 1 || d.sat[255] != 255 {
		tv := d.Transfers()
		t.Fatalf("sat[1]=%d sat[255]=%d transfers %+v hclock %d scanline %d hmode %d allowDMA %v", d.sat[1], d.sat[255], tv, d.hclock, d.scanline, d.hMode, d.allowDMA)
	}
	if d.status&StatusSATBDone == 0 {
		t.Fatal("SATB completion flag not set after the transfer")
	}
}

func TestTilePixelDecodesPlanes(t *testing.T) {
	// plane0=0x80 (bit7 of low byte → x=0), plane1=0x01<<8 for x=7, plane2 high, plane3.
	p01 := uint16(0x0180) // plane0: x0 set; plane1: x7 set
	p23 := uint16(0x8001) // plane2: x7 set; plane3: x0 set
	if got := tilePixel(p01, p23, 0); got != 0x09 {
		t.Fatalf("x0 = %X, want 9 (planes 0 and 3)", got)
	}
	if got := tilePixel(p01, p23, 7); got != 0x06 {
		t.Fatalf("x7 = %X, want 6 (planes 1 and 2)", got)
	}
}

// Spec §5.1: a VWR write waits in the queue (Q1/Q2), is served at the first
// free slot after the delay (Q4/Q7), and a second access stalls the CPU
// until then (Q8). Fresh VDC: VDS, scanline 0, hclock 0, divider 4.
func TestVRAMWriteIsQueuedAndServedAtAFreeSlot(t *testing.T) {
	d, _ := newVDC()
	d.Stall = func() { d.Advance(3) }
	d.set(0x00, 0x1234)
	d.selectReg(0x02)
	d.writeWord(0xBEEF)
	if d.vram[0x1234] != 0 || !d.pendWrite || d.Read(0)&StatusBusy == 0 {
		t.Fatalf("write must be queued: vram=%04X pending=%v", d.vram[0x1234], d.pendWrite)
	}
	// 21 master clocks of delay, then the first 8 dots of the line block:
	// hclock 30 is still too early, hclock 33 (dot 8, even) serves it.
	d.Advance(30)
	if d.vram[0x1234] != 0 {
		t.Fatalf("served too early at hclock %d", d.hclock)
	}
	d.Advance(6)
	if d.vram[0x1234] != 0xBEEF || d.reg[0] != 0x1235 || d.pendWrite {
		t.Fatalf("not served by hclock %d: vram=%04X mawr=%04X", d.hclock, d.vram[0x1234], d.reg[0])
	}
	// Two writes back to back: the second stalls until the first lands.
	d.writeWord(0x1111)
	d.writeWord(0x2222)
	if d.vram[0x1235] != 0x1111 || d.reg[0] != 0x1236 || !d.pendWrite {
		t.Fatalf("second write must stall for the first: vram[1235]=%04X mawr=%04X pending=%v", d.vram[0x1235], d.reg[0], d.pendWrite)
	}
	d.waitAccess()
	if d.vram[0x1236] != 0x2222 || d.reg[0] != 0x1237 {
		t.Fatalf("vram[1236]=%04X mawr=%04X", d.vram[0x1236], d.reg[0])
	}
	// Reads queue too, and reading the data port waits for the prefetch.
	d.set(0x01, 0x1235)
	if !d.pendRead {
		t.Fatal("MARR write must queue a read")
	}
	d.selectReg(0x02)
	if v := d.readWord(); v != 0x1111 {
		t.Fatalf("read %04X", v)
	}
}

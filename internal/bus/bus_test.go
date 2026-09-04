package bus

import (
	"testing"

	"github.com/wicanr2/onepce-ai-remake/internal/addr"
)

// fakeROM has n banks; byte 0 of each bank is the bank number.
func fakeROM(n int) []byte {
	rom := make([]byte, n*bankSize)
	for i := 0; i < n; i++ {
		rom[i*bankSize] = uint8(i)
	}
	return rom
}

func TestNewRejectsPartialBanks(t *testing.T) {
	if _, err := New(make([]byte, bankSize+1), Devices{}); err == nil {
		t.Fatal("a ROM that is not a whole number of banks must be refused")
	}
}

func Test384KMirroring(t *testing.T) {
	b, err := New(fakeROM(0x30), Devices{})
	if err != nil {
		t.Fatal(err)
	}
	// Spec B3: $00–$1F → 0..$1F, $20–$3F → 0..$1F again, $40–$7F → $20..$2F ×4.
	cases := map[uint8]uint8{
		0x00: 0x00, 0x1F: 0x1F, 0x20: 0x00, 0x3F: 0x1F,
		0x40: 0x20, 0x4F: 0x2F, 0x50: 0x20, 0x6A: 0x2A, 0x7F: 0x2F,
	}
	for bank, want := range cases {
		b.SetMPR(1<<3, bank) // map it at page 3 ($6000)
		if got := b.Read(0x6000); got != want {
			t.Errorf("physical bank $%02X reads ROM bank $%02X, want $%02X", bank, got, want)
		}
		if got := b.FileOffset(uint32(bank) << 13); got != int64(want)*bankSize {
			t.Errorf("FileOffset(bank $%02X) = %d, want %d", bank, got, int64(want)*bankSize)
		}
	}
}

func TestPowerOfTwoROMWrapsAndUnmappedReadsFF(t *testing.T) {
	b, _ := New(fakeROM(4), Devices{})
	b.SetMPR(1<<2, 0x05) // page 2 ← bank 5 → ROM bank 1
	if got := b.Read(0x4000); got != 1 {
		t.Fatalf("bank 5 of a 4-bank ROM reads %d, want 1", got)
	}
	b.SetMPR(1<<2, 0x80)
	if got := b.Read(0x4000); got != 0xFF {
		t.Fatalf("unmapped bank reads %02X, want FF", got)
	}
	if b.FileOffset(0x80<<13) != addr.FileUnknown {
		t.Fatal("unmapped bank must have no file offset")
	}
}

func TestWorkRAMIsMirroredAndROMIsReadOnly(t *testing.T) {
	b, _ := New(fakeROM(2), Devices{})
	b.SetMPR(1<<1, 0xF8)
	b.SetMPR(1<<2, 0xFB)
	b.Write(0x2010, 0x42)
	if b.Read(0x4010) != 0x42 {
		t.Fatal("$FB must mirror $F8")
	}
	b.SetMPR(1<<3, 0x00)
	b.Write(0x6000, 0x99)
	if b.Read(0x6000) != 0 {
		t.Fatal("ROM must ignore writes")
	}
	if got := b.Resolve(0x2010); got.File != addr.FileUnknown || got.Physical != 0x1F0010 {
		t.Fatalf("Resolve($2010) = %s", got)
	}
}

func TestTimerCountsEvery1024CyclesAndRaisesIRQ(t *testing.T) {
	b, _ := New(fakeROM(1), Devices{})
	b.SetMPR(1<<0, 0xFF)
	b.Write(0x0C00, 2) // reload 2
	b.Write(0x0C01, 1) // enable → counter = 2
	b.SetSpeed(true)   // one CPU cycle = one timer step at high speed
	for i := 0; i < 3*1024; i++ {
		b.Tick(1)
	}
	// 3 expiries: 2→1, 1→0, 0→reload+IRQ.
	if b.PendingIRQ()&irqTimer == 0 {
		t.Fatal("timer IRQ not raised after counter passed zero")
	}
	if b.Read(0x0C00)&0x7F != 2 {
		t.Fatalf("counter = %d after reload, want 2", b.Read(0x0C00)&0x7F)
	}
	b.Write(0x1402, 0x04) // mask the timer
	if b.PendingIRQ() != 0 {
		t.Fatal("$1402 bit2 must mask the timer line")
	}
	b.Write(0x1402, 0)
	b.Write(0x1403, 0) // acknowledge
	if b.PendingIRQ() != 0 {
		t.Fatal("$1403 write must clear the timer line")
	}
}

func TestVDCAccessStallsOneCycle(t *testing.T) {
	b, _ := New(fakeROM(1), Devices{})
	b.SetMPR(1<<0, 0xFF)
	before := b.MasterCycles()
	b.Read(0x0000)
	if b.MasterCycles()-before != 24 { // the access cycle plus one stall cycle, slow speed
		t.Fatalf("VDC read advanced %d master cycles, want 24", b.MasterCycles()-before)
	}
	b.SetSpeed(true)
	before = b.MasterCycles()
	b.WriteVDCPort(0, 0)
	if b.MasterCycles()-before != 6 {
		t.Fatalf("ST0 at high speed advanced %d, want 6", b.MasterCycles()-before)
	}
	before = b.MasterCycles()
	b.SetMPR(1<<1, 0xF8)
	b.Read(0x2000)
	if b.MasterCycles()-before != 3 {
		t.Fatalf("RAM read advanced %d, want 3 (one cycle, no stall)", b.MasterCycles()-before)
	}
}

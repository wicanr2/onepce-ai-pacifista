package onepce

import (
	"testing"
)

// testROM assembles a one-bank HuCard whose reset vector points at $E000 and
// whose code is the given bytes. MPR7 is 0 at power-on, so $E000 maps to file
// offset 0.
func testROM(code ...uint8) []byte {
	rom := make([]byte, 0x2000)
	copy(rom, code)
	rom[0x1FFE], rom[0x1FFF] = 0x00, 0xE0
	return rom
}

// A program that maps work RAM at $2000 (TAM #2 ← $F8), the I/O page at
// $0000 (TAM #1 ← $FF), writes a byte to zero page, writes one VRAM word
// through the VDC ports, then loops forever.
var probeProgram = []uint8{
	0xA9, 0xF8, 0x53, 0x02, // LDA #$F8 ; TAM #2
	0xA9, 0xFF, 0x53, 0x01, // LDA #$FF ; TAM #1
	0xA9, 0x42, 0x85, 0x10, // LDA #$42 ; STA $10        ($8008..$800B)
	0x03, 0x00, // ST0 #0  (select MAWR)              ($800C)
	0x13, 0x34, 0x23, 0x12, // ST1 #$34 ; ST2 #$12   → MAWR = $1234
	0x03, 0x02, // ST0 #2  (select VWR)
	0x13, 0xEF, 0x23, 0xBE, // ST1 #$EF ; ST2 #$BE   → VRAM[$1234] = $BEEF ($8014..$8017)
	0x80, 0xFE, // BRA *                                 ($8018)
}

func loadProbe(t *testing.T) *Machine {
	t.Helper()
	m, err := Load(testROM(probeProgram...))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func run(m *Machine, steps int) {
	for i := 0; i < steps; i++ {
		m.Step()
	}
}

func TestWriteWatchReportsInstructionStartAndResolvedAddress(t *testing.T) {
	m := loadProbe(t)
	var got []Event
	w := m.Watch(Write, CPU, 0x2000, 0x20FF, func(e Event) { got = append(got, e) })
	run(m, 8)
	if len(got) != 1 || w.Count() != 1 {
		t.Fatalf("events %d", len(got))
	}
	e := got[0]
	if e.PC != 0xE00A || e.Opcode != 0x85 || e.Value != 0x42 {
		t.Fatalf("event %+v", e)
	}
	if e.Addr.Logical != 0x2010 || e.Addr.Physical != 0x1F0010 || e.Addr.File != FileUnknown {
		t.Fatalf("address %s", e.Addr)
	}
	if e.Addr.MPR[1] != 0xF8 || e.Addr.MPR[0] != 0xFF {
		t.Fatalf("mpr %v", e.Addr.MPR)
	}
}

func TestVRAMWatchSeesCPUPortWritesWithSource(t *testing.T) {
	m := loadProbe(t)
	var got []Event
	m.Watch(Write, VRAM, 0x1234, 0x1234, func(e Event) { got = append(got, e) })
	run(m, 12)
	if len(got) != 1 {
		t.Fatalf("vram events %d", len(got))
	}
	if got[0].Value != 0xBEEF || got[0].Source != ByCPU || got[0].PC != 0xE016 {
		t.Fatalf("event %+v", got[0])
	}
}

func TestExecWatchLimitAndIgnore(t *testing.T) {
	m := loadProbe(t)
	hits := 0
	w := m.Watch(Exec, CPU, 0xE018, 0xE018, func(e Event) { hits++ }).Limit(3)
	run(m, 30) // the BRA loop runs many times
	if hits != 3 || w.Count() != 3 || w.Skipped() == 0 {
		t.Fatalf("hits=%d count=%d skipped=%d", hits, w.Count(), w.Skipped())
	}
	m2 := loadProbe(t)
	w2 := m2.Watch(Exec, CPU, 0xE000, 0xFFFF, func(Event) {}).IgnorePC(0xE018)
	run(m2, 30)
	if w2.Ignored() == 0 || w2.Count() != 12 {
		t.Fatalf("ignored=%d count=%d (want the 12 straight-line instructions)", w2.Ignored(), w2.Count())
	}
}

func TestTraceHashIsDeterministic(t *testing.T) {
	a, b := loadProbe(t), loadProbe(t)
	ha, hb := a.NewTraceHash(), b.NewTraceHash()
	run(a, 50)
	run(b, 50)
	sa, na := ha.Sum()
	sb, nb := hb.Sum()
	if sa != sb || na != 50 || nb != 50 {
		t.Fatalf("%s/%d vs %s/%d", sa, na, sb, nb)
	}
}

func TestSnapshotSectionsAndDiff(t *testing.T) {
	m := loadProbe(t)
	before := m.Snapshot()
	run(m, 12)
	// The VRAM word sits in the VDC's access queue until a free slot
	// (docs/spec/vdc-vce.md §5.1); the probe's final loop lets it land.
	run(m, 40)
	after := m.Snapshot()
	if after.RAM[0x10] != 0x42 || after.VRAM[0x1234] != 0xBEEF {
		t.Fatal("snapshot did not capture the writes")
	}
	diff := after.Diff(before)
	want := map[Section]bool{SectionRAM: true, SectionVRAM: true, SectionVDCRegs: true, SectionCPU: true}
	if len(diff) != len(want) {
		t.Fatalf("diff %v", diff)
	}
	for _, s := range diff {
		if !want[s] {
			t.Fatalf("unexpected section %s in diff %v", s, diff)
		}
	}
	if after.ROMHash != m.ROMHash() || after.Version != Version {
		t.Fatal("snapshot provenance missing")
	}
}

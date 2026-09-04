package onepce

import (
	"bytes"
	"strings"
	"testing"
)

// A program that keeps the machine busy in every component: it maps RAM and
// I/O, enables the timer, writes a VRAM word per loop and counts in RAM.
var busyProgram = []uint8{
	0xA9, 0xF8, 0x53, 0x02, // LDA #$F8 ; TAM #2
	0xA9, 0xFF, 0x53, 0x01, // LDA #$FF ; TAM #1
	0xA9, 0x03, 0x8D, 0x00, 0x0C, // LDA #3 ; STA $0C00 (timer reload)
	0xA9, 0x01, 0x8D, 0x01, 0x0C, // LDA #1 ; STA $0C01 (timer on)
	0xD4,       // CSH
	0x03, 0x00, // ST0 #0  (MAWR)
	0x13, 0x00, 0x23, 0x10, // MAWR = $1000
	0x03, 0x02, // ST0 #2  (VWR)
	// loop: INC $20 ; LDA $20 ; ST1 via STA $0002 ; STA $0003 ; BRA loop
	0xE6, 0x20, 0xA5, 0x20, 0x8D, 0x02, 0x00, 0x8D, 0x03, 0x00, 0x80, 0xF4,
}

// S5: save → load → run N equals running N without the round trip.
func TestSaveStateRoundTripIsDeterministic(t *testing.T) {
	rom := testROM(busyProgram...)
	a, err := Load(rom)
	if err != nil {
		t.Fatal(err)
	}
	a.Schedule(Press{Frame: 2, Button: ButtonI, Span: 3})
	a.RunFrames(3)
	run(a, 777)

	var buf bytes.Buffer
	if err := a.SaveState(&buf); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(buf.String(), `{"format":1,`) {
		t.Fatalf("header: %q", buf.String()[:40])
	}

	b, err := Load(rom)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.LoadState(bytes.NewReader(buf.Bytes())); err != nil {
		t.Fatal(err)
	}
	if b.Frame() != a.Frame() || len(b.Plan()) != 1 {
		t.Fatalf("frame %d vs %d, plan %v", b.Frame(), a.Frame(), b.Plan())
	}

	a.RunFrames(2)
	run(a, 1234)
	b.RunFrames(2)
	run(b, 1234)
	sa, sb := a.Snapshot(), b.Snapshot()
	for _, sec := range AllSections {
		if sa.Hashes[sec] != sb.Hashes[sec] {
			t.Fatalf("section %s differs after the round trip", sec)
		}
	}
	if sa.CPU != sb.CPU || sa.Frame != sb.Frame {
		t.Fatalf("cpu %+v vs %+v", sa.CPU, sb.CPU)
	}
}

func TestLoadStateRejectsOtherROMsAndFormats(t *testing.T) {
	a, _ := Load(testROM(busyProgram...))
	var buf bytes.Buffer
	if err := a.SaveState(&buf); err != nil {
		t.Fatal(err)
	}
	other, _ := Load(testROM(probeProgram...))
	if err := other.LoadState(bytes.NewReader(buf.Bytes())); err == nil {
		t.Fatal("a savestate from another ROM must be refused")
	}
	bad := strings.Replace(buf.String(), `{"format":1,`, `{"format":2,`, 1)
	if err := a.LoadState(strings.NewReader(bad)); err == nil {
		t.Fatal("a different format version must be refused")
	}
}

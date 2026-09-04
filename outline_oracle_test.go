package onepce

import (
	"os"
	"testing"
)

// Oracle acceptance for the observe layer (docs/spec/observe.md): redo the
// nectaris-cht re/203 finding with watches instead of Lua. Route (P-100):
// RUN through the title into REVOLT, then from frame 2500 every 20 frames
// right×7, down×4, I, I — the move range is on screen by frame 2800. Every
// map-tile VRAM word ($5480–$7920, P-131) the CPU rewrote to draw it must come
// from the outline routine at $A1F5–$A3CF, and the rewrite must only set bits
// (the original ORs the mask into all four planes, P-131/P-159). Needs
// ONEPCE_ROM; skips otherwise.
func TestMoveRangeOutlineWriterIsTheA1F5Routine(t *testing.T) {
	romPath := os.Getenv("ONEPCE_ROM")
	if romPath == "" {
		t.Skip("ONEPCE_ROM not set: oracle test skipped")
	}
	rom, err := os.ReadFile(romPath)
	if err != nil {
		t.Fatal(err)
	}
	m, err := Load(rom)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range []uint64{1680, 1815, 1950, 2085, 2220, 2355} {
		m.Schedule(Press{Frame: f, Button: ButtonRun, Span: 15})
	}
	frame := uint64(2500)
	for i := 0; i < 7; i++ {
		m.Schedule(Press{Frame: frame, Button: ButtonRight, Span: 8})
		frame += 20
	}
	for i := 0; i < 4; i++ {
		m.Schedule(Press{Frame: frame, Button: ButtonDown, Span: 8})
		frame += 20
	}
	m.Schedule(Press{Frame: frame, Button: ButtonI, Span: 8})
	frame += 20
	m.Schedule(Press{Frame: frame, Button: ButtonI, Span: 8})

	m.RunToFrame(2500)
	base := m.Snapshot(SectionVRAM)

	type hit struct {
		ev  Event
		old uint16
	}
	var changed []hit
	var outside []Event
	// Only the map tile area ($5480–$7920, P-131): the cursor moves also
	// rewrite BAT and staging areas elsewhere in VRAM, by other routines.
	w := m.Watch(Write, VRAM, 0x5480, 0x7920, func(e Event) {
		old := base.VRAM[e.Addr.Logical]
		if e.Source != ByCPU || old == e.Value {
			return
		}
		changed = append(changed, hit{e, old})
		if e.PC < 0xA1F5 || e.PC > 0xA3CF {
			outside = append(outside, e)
		}
	}).Limit(200000)
	m.RunToFrame(2800)
	w.Remove()

	if len(changed) == 0 {
		t.Fatalf("no VRAM word changed between frames 2500 and 2800 (skipped %d): the route did not reach the move range", w.Skipped())
	}
	setsOnly := 0
	for _, h := range changed {
		if h.old&^h.ev.Value == 0 {
			setsOnly++
		}
	}
	t.Logf("%d changed words, %d of them set-only, %d skipped events, %d outside $A1F5–$A3CF", len(changed), setsOnly, w.Skipped(), len(outside))
	if len(outside) > 0 {
		e := outside[0]
		t.Fatalf("%d rewrites came from outside the outline routine; first: %s value %04X frame %d", len(outside), m.Resolve(e.PC), e.Value, e.Frame)
	}
	if setsOnly != len(changed) {
		t.Fatalf("%d rewrites cleared bits; the outline is composited with ORA", len(changed)-setsOnly)
	}
	after := m.Snapshot(SectionVRAM)
	if after.Hashes[SectionVRAM] == base.Hashes[SectionVRAM] {
		t.Fatal("VRAM hash unchanged although writes were observed")
	}
}

package gui

import (
	"image"
	"image/color"
	"testing"

	"github.com/wicanr2/onepce-ai-remake"
)

// A ROM that maps RAM, enables nothing and loops: enough for frames to pass.
func testROM() []byte {
	rom := make([]byte, 0x2000)
	copy(rom, []byte{0xA9, 0xF8, 0x53, 0x02, 0xA9, 0xFF, 0x53, 0x01, 0x80, 0xFE})
	rom[0x1FFE], rom[0x1FFF] = 0x00, 0xE0
	return rom
}

func TestPauseAndInputRecording(t *testing.T) {
	m, err := onepce.Load(testROM())
	if err != nil {
		t.Fatal(err)
	}
	s := New(m)
	s.Tick(0)
	if m.Frame() != 1 {
		t.Fatalf("one tick must run one frame, frame %d", m.Frame())
	}
	s.Paused = true
	s.Tick(0)
	if m.Frame() != 1 {
		t.Fatal("paused tick must not advance")
	}
	s.StepFrame()
	if m.Frame() != 2 {
		t.Fatal("StepFrame")
	}
	s.Paused = false
	// Hold RUN for three ticks, then I for one.
	for i := 0; i < 3; i++ {
		s.Tick(onepce.ButtonRun)
	}
	s.Tick(onepce.ButtonI)
	s.Tick(0)
	plan := s.Plan()
	if len(plan) != 2 || plan[0] != (onepce.Press{Frame: 3, Button: onepce.ButtonRun, Span: 3}) || plan[1] != (onepce.Press{Frame: 6, Button: onepce.ButtonI, Span: 1}) {
		t.Fatalf("plan %+v", plan)
	}
	if FormatPresses(plan) != "3:run:3,6:i:1" {
		t.Fatalf("format %q", FormatPresses(plan))
	}
}

// Spec G1/§4: replaying the recorded plan headless reaches the same state.
func TestReplayOfTheRecordedPlanMatchesTheSession(t *testing.T) {
	m, _ := onepce.Load(testROM())
	s := New(m)
	for i := 0; i < 10; i++ {
		held := uint8(0)
		if i >= 2 && i < 6 {
			held = onepce.ButtonRight
		}
		s.Tick(held)
	}
	plan := s.Plan()
	h, _ := onepce.Load(testROM())
	h.Schedule(plan...)
	h.RunToFrame(m.Frame())
	a, b := m.Snapshot(onepce.SectionCPU, onepce.SectionRAM), h.Snapshot(onepce.SectionCPU, onepce.SectionRAM)
	if a.Hashes[onepce.SectionCPU] != b.Hashes[onepce.SectionCPU] || a.Hashes[onepce.SectionRAM] != b.Hashes[onepce.SectionRAM] {
		t.Fatalf("session and headless replay differ at frame %d", m.Frame())
	}
}

func TestDiffAndReferenceIndex(t *testing.T) {
	native := image.NewRGBA(image.Rect(0, 0, 4, 2))
	for y := 0; y < 2; y++ {
		for x := 0; x < 4; x++ {
			native.Set(x, y, color.RGBA{R: uint8(x * 60), G: 0, B: 0, A: 255})
		}
	}
	// Reference canvas at 3× with offset (3, 0): pixel (x,y) maps to (3+3x+1, 3y+1).
	ref := image.NewRGBA(image.Rect(0, 0, 3+12, 6))
	for y := 0; y < 6; y++ {
		for x := 0; x < 15; x++ {
			nx := (x - 3) / 3
			if x < 3 {
				nx = 0
			}
			c := color.RGBA{R: uint8(nx * 60), A: 255}
			if nx == 2 {
				c.R += 20 // one native column differs beyond the tolerance
			}
			ref.Set(x, y, c)
		}
	}
	out, n := Diff(native, ref, 3, 3, 0)
	if n != 2 {
		t.Fatalf("%d differing pixels, want 2", n)
	}
	if r, g, b, _ := out.At(2, 0).RGBA(); r>>8 != 255 || g != 0 || b>>8 != 255 {
		t.Fatal("differing pixel not painted magenta")
	}
	r := &Reference{Paths: []string{"a", "b", "c"}, Start: 100, Every: 2}
	for frame, want := range map[uint64]int{0: 0, 100: 0, 101: 0, 102: 1, 105: 2, 999: 2} {
		if got := r.Index(frame); got != want {
			t.Fatalf("index(%d) = %d, want %d", frame, got, want)
		}
	}
	if _, _, _, _, _, err := ParseWatch("write:vram:5480-7920:100"); err != nil {
		t.Fatal(err)
	}
	if _, _, _, _, _, err := ParseWatch("bogus"); err == nil {
		t.Fatal("bad watch must error")
	}
}

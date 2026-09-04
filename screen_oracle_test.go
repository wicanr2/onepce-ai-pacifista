package onepce

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/wicanr2/onepce-ai-remake/oracle"
)

// Oracle acceptance for docs/spec/framebuffer-parity.md: every pixel of the
// framebuffer equals Mesen2's picture at the same frame, at the position the
// display window predicts. Fixtures come from tools/oracle/mesen2_state_probe.lua
// (screen-<frame>.bin plus vram/sat/palette dumps); ONEPCE_SCREEN_PRESS is the
// same plan the probe was run with.
func TestFramebufferMatchesMesen2Picture(t *testing.T) {
	romPath, dir, plan := os.Getenv("ONEPCE_ROM"), os.Getenv("ONEPCE_SCREEN_FIXTURES"), os.Getenv("ONEPCE_SCREEN_PRESS")
	if romPath == "" || dir == "" || plan == "" {
		t.Skip("ONEPCE_ROM, ONEPCE_SCREEN_FIXTURES and ONEPCE_SCREEN_PRESS not all set")
	}
	rom, err := os.ReadFile(romPath)
	if err != nil {
		t.Fatal(err)
	}
	screens, err := filepath.Glob(filepath.Join(dir, "screen-*.bin"))
	if err != nil || len(screens) == 0 {
		t.Fatalf("no screen-*.bin in %s (%v)", dir, err)
	}
	presses, err := ParsePresses(plan)
	if err != nil {
		t.Fatal(err)
	}
	m, err := Load(rom)
	if err != nil {
		t.Fatal(err)
	}
	m.Schedule(presses...)

	frames := map[uint64]string{}
	var order []uint64
	for _, p := range screens {
		var f uint64
		if _, err := fmtSscanf(filepath.Base(p), &f); err != nil {
			t.Fatalf("fixture name %s: %v", p, err)
		}
		frames[f] = p
		order = append(order, f)
	}
	sortUint64(order)

	for _, f := range order {
		m.RunToFrame(f)
		if m.Frame() != f {
			t.Fatalf("frame %d: machine at %d", f, m.Frame())
		}
		// Memory first: a picture difference on top of a memory difference is
		// a machine problem, not a renderer problem.
		snap := m.Snapshot(SectionVRAM, SectionSAT, SectionVCE)
		for _, sec := range []struct {
			name string
			got  []uint16
		}{{"vram", snap.VRAM}, {"sat", snap.SAT}, {"palette", snap.VCE}} {
			want := readWords(t, filepath.Join(dir, sec.name+"-"+u64(f)+".bin"))
			if len(want) == 0 {
				continue
			}
			if diff := countDiff(sec.got, want); diff != 0 {
				t.Fatalf("frame %d: %s differs from Mesen2 in %d words", f, sec.name, diff)
			}
		}

		sf, err := os.Open(frames[f])
		if err != nil {
			t.Fatal(err)
		}
		ref, err := oracle.ReadMesen2Screen(sf)
		sf.Close()
		if err != nil {
			t.Fatal(err)
		}
		w, h, px := m.FramebufferNative()
		dot0, line0 := m.DisplayWindow()
		x0 := dot0 - oracle.Mesen2LeftOverscan(m.ClockDivider())
		y0 := line0 - 14
		got := oracle.MatchScreen(w, h, px, ref, x0, y0)
		// Mesen2 outputs scanlines 14–255 only; a display window that runs
		// to scanline 256 has its last row outside the reference.
		inside := 0
		for y := 0; y < h; y++ {
			if ry := y + y0; ry >= 0 && ry < ref.H {
				for x := 0; x < w; x++ {
					if rx := x + x0; rx >= 0 && rx < ref.W {
						inside++
					}
				}
			}
		}
		if got.Compared != inside || inside < w*(h-1) {
			t.Errorf("frame %d: %d pixels compared, %d inside the %dx%d reference at (%d,%d), native %dx%d", f, got.Compared, inside, ref.W, ref.H, x0, y0, w, h)
		}
		if got.Mismatch != 0 {
			best := oracle.SearchScreen(w, h, px, ref, x0, y0, 4)
			t.Errorf("frame %d: %d of %d pixels differ at the predicted (%d,%d); best within ±4 is (%d,%d) with %d",
				f, got.Mismatch, got.Compared, x0, y0, best.X0, best.Y0, best.Mismatch)
			continue
		}
		t.Logf("frame %d: %dx%d native at Mesen2 (%d,%d) divider %d: %d pixels identical (%d outside the reference), %d colours",
			f, w, h, x0, y0, m.ClockDivider(), got.Compared, w*h-inside, got.Colours)
	}
}

func readWords(t *testing.T, path string) []uint16 {
	t.Helper()
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	w := make([]uint16, len(b)/2)
	for i := range w {
		w[i] = binary.LittleEndian.Uint16(b[2*i:])
	}
	return w
}

func countDiff(a, b []uint16) int {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	d := len(a) - n + len(b) - n
	for i := 0; i < n; i++ {
		if a[i] != b[i] {
			d++
		}
	}
	return d
}

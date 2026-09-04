package onepce

import (
	"bytes"
	"os"
	"testing"
)

// Oracle acceptance for docs/spec/psg.md §6.2: the VGM this machine records
// over a frame window is byte for byte the one nectaris-cht's Mesen2 probe
// (tools/mesen2_pce_psg_vgm_probe.lua) recorded for the same window and
// input plan. ONEPCE_VGM_FIXTURE names the probe's .vgm, ONEPCE_VGM_WINDOW
// is "start-stop", ONEPCE_VGM_PRESS the plan given to the probe.
func TestVGMRecordingMatchesTheMesen2Probe(t *testing.T) {
	romPath, fixture, window, plan := os.Getenv("ONEPCE_ROM"), os.Getenv("ONEPCE_VGM_FIXTURE"), os.Getenv("ONEPCE_VGM_WINDOW"), os.Getenv("ONEPCE_VGM_PRESS")
	if romPath == "" || fixture == "" || window == "" {
		t.Skip("ONEPCE_ROM, ONEPCE_VGM_FIXTURE and ONEPCE_VGM_WINDOW not all set")
	}
	rom, err := os.ReadFile(romPath)
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatal(err)
	}
	var start, stop uint64
	if _, err := fmtSscanfWindow(window, &start, &stop); err != nil || stop <= start {
		t.Fatalf("ONEPCE_VGM_WINDOW %q: want start-stop", window)
	}
	m, err := Load(rom)
	if err != nil {
		t.Fatal(err)
	}
	presses, err := ParsePresses(plan)
	if err != nil {
		t.Fatal(err)
	}
	m.Schedule(presses...)
	m.RecordVGM(start, stop)
	for {
		if _, done := m.VGM(); done {
			break
		}
		if m.Frame() > stop+2 {
			t.Fatalf("recording window never closed by frame %d", m.Frame())
		}
		m.RunFrames(1)
	}
	got, _ := m.VGM()
	if bytes.Equal(got, want) {
		t.Logf("VGM identical: %d bytes, frames %d–%d", len(got), start, stop)
		return
	}
	n := len(got)
	if len(want) < n {
		n = len(want)
	}
	first := -1
	for i := 0; i < n; i++ {
		if got[i] != want[i] {
			first = i
			break
		}
	}
	lo := first - 8
	if lo < 0 {
		lo = 0
	}
	hi := first + 16
	if hi > n {
		hi = n
	}
	t.Fatalf("VGM differs: ours %d bytes, oracle %d bytes, first difference at offset %d (ours % X, oracle % X)",
		len(got), len(want), first, got[lo:hi], want[lo:hi])
}

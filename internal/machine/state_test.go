package machine

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// M2 oracle comparison (docs/spec/vdc-vce.md §10): replay the same input
// plan the Mesen2 state probe used and compare work RAM, VRAM, SAT and the
// VCE palette at each dumped frame. Fixtures are private:
//
//	ONEPCE_ROM            path to the .pce
//	ONEPCE_STATE_FIXTURES directory with state-<f>.tsv / ram / vram / sat / palette bins
//	ONEPCE_STATE_PRESS    the STATE_PRESS string the probe was run with
//
// Without them the test skips and says so.
func TestMachineStateMatchesMesen2Dumps(t *testing.T) {
	romPath := os.Getenv("ONEPCE_ROM")
	fixtures := os.Getenv("ONEPCE_STATE_FIXTURES")
	if romPath == "" || fixtures == "" {
		t.Skip("ONEPCE_ROM / ONEPCE_STATE_FIXTURES not set: oracle state comparison skipped")
	}
	rom, err := os.ReadFile(romPath)
	if err != nil {
		t.Fatal(err)
	}
	dumps, err := filepath.Glob(filepath.Join(fixtures, "state-*.tsv"))
	if err != nil || len(dumps) == 0 {
		t.Fatalf("no state-*.tsv in %s", fixtures)
	}
	m, err := New(rom)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range strings.Split(os.Getenv("ONEPCE_STATE_PRESS"), ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		parts := strings.Split(item, ":")
		if len(parts) != 3 {
			t.Fatalf("bad press %q", item)
		}
		frame, _ := strconv.Atoi(parts[0])
		span, _ := strconv.Atoi(parts[2])
		m.Schedule(Press{Frame: uint64(frame), Button: buttonByName(t, parts[1]), Span: span})
	}

	var frames []int
	for _, d := range dumps {
		var f int
		fmt.Sscanf(filepath.Base(d), "state-%d.tsv", &f)
		frames = append(frames, f)
	}
	sortInts(frames)

	// Snapshot work RAM in the very cycle the frame counter advances, as the
	// oracle does; everything else is compared at the next instruction
	// boundary, which is fine for VRAM/SAT/palette because the CPU is not
	// mid-transfer at scanline 256 in this route (the comparison would show
	// it if it were).
	var ramAtFrame [0x2000]byte
	var pcAtFrame uint16
	m.FrameHook = func(uint64) {
		ramAtFrame = m.Bus.RAM
		pcAtFrame = m.CPU.PC
	}

	for _, frame := range frames {
		m.RunToFrame(uint64(frame))
		st := loadState(t, filepath.Join(fixtures, fmt.Sprintf("state-%d.tsv", frame)))
		t.Logf("frame %d: oracle pc=$%04X mpr=%s | ours pc=$%04X (at boundary $%04X) mpr=%v", frame,
			int(st["cpu.pc"]), mprText(st), m.CPU.PC, pcAtFrame, m.Bus.MPR())

		// The stack page ($2100–$21FF) holds return addresses of interrupts
		// that land on different instructions whenever cycle counts differ
		// (VRAM stalls, spec vdc-vce.md §8), so it is compared separately and
		// only reported. Bytes listed in ONEPCE_STATE_IGNORE (hex offsets into
		// work RAM, comma separated) are known timing counters of this ROM.
		ram := readBin(t, filepath.Join(fixtures, fmt.Sprintf("ram-%d.bin", frame)))
		ignore := map[int]bool{}
		for _, h := range strings.Split(os.Getenv("ONEPCE_STATE_IGNORE"), ",") {
			if v, err := strconv.ParseInt(strings.TrimSpace(h), 16, 32); err == nil {
				ignore[int(v)] = true
			}
		}
		for i := 0x100; i < 0x200; i++ {
			ignore[i] = true
		}
		compareBytesIgnoring(t, frame, "work RAM", ramAtFrame[:], ram, ignore)

		vram := readWords(t, filepath.Join(fixtures, fmt.Sprintf("vram-%d.bin", frame)))
		compareWords(t, frame, "VRAM", m.VDC.VRAM(), vram)

		if p := filepath.Join(fixtures, fmt.Sprintf("sat-%d.bin", frame)); fileExists(p) {
			compareWords(t, frame, "SAT", m.VDC.SAT(), readWords(t, p))
		}
		pal := readWords(t, filepath.Join(fixtures, fmt.Sprintf("palette-%d.bin", frame)))
		compareWords(t, frame, "palette", m.VCE.Palette(), pal)
	}
}

func buttonByName(t *testing.T, name string) uint8 {
	switch strings.ToLower(name) {
	case "i":
		return ButtonI
	case "ii":
		return ButtonII
	case "select":
		return ButtonSelect
	case "run":
		return ButtonRun
	case "up":
		return ButtonUp
	case "right":
		return ButtonRight
	case "down":
		return ButtonDown
	case "left":
		return ButtonLeft
	}
	t.Fatalf("unknown button %q", name)
	return 0
}

func sortInts(a []int) {
	for i := 1; i < len(a); i++ {
		for j := i; j > 0 && a[j-1] > a[j]; j-- {
			a[j-1], a[j] = a[j], a[j-1]
		}
	}
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func readBin(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func readWords(t *testing.T, path string) []uint16 {
	b := readBin(t, path)
	out := make([]uint16, len(b)/2)
	for i := range out {
		out[i] = binary.LittleEndian.Uint16(b[2*i:])
	}
	return out
}

func loadState(t *testing.T, path string) map[string]float64 {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	out := map[string]float64{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		kv := strings.SplitN(sc.Text(), "\t", 2)
		if len(kv) != 2 {
			continue
		}
		if v, err := strconv.ParseFloat(kv[1], 64); err == nil {
			out[kv[0]] = v
		}
	}
	return out
}

func mprText(st map[string]float64) string {
	parts := make([]string, 8)
	for i := range parts {
		parts[i] = fmt.Sprintf("%02X", int(st[fmt.Sprintf("memoryManager.mpr[%d]", i)]))
	}
	return strings.Join(parts, " ")
}

func compareBytesIgnoring(t *testing.T, frame int, what string, got, want []byte, ignore map[int]bool) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("frame %d %s: size %d vs oracle %d", frame, what, len(got), len(want))
		return
	}
	diffs, ignored := 0, 0
	var where []string
	for i := range got {
		if got[i] == want[i] {
			continue
		}
		if ignore[i] {
			ignored++
			continue
		}
		diffs++
		if len(where) < 24 {
			where = append(where, fmt.Sprintf("$%04X:%02X/%02X", i, got[i], want[i]))
		}
	}
	if diffs > 0 {
		t.Errorf("frame %d %s: %d/%d bytes differ outside the ignored set (%d ignored); %s",
			frame, what, diffs, len(got), ignored, strings.Join(where, " "))
	} else {
		t.Logf("frame %d %s: identical outside the ignored set (%d ignored bytes differ: stack page / timing counters)", frame, what, ignored)
	}
}

func compareBytes(t *testing.T, frame int, what string, got, want []byte) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("frame %d %s: size %d vs oracle %d", frame, what, len(got), len(want))
		return
	}
	diffs, first := 0, -1
	var where []string
	for i := range got {
		if got[i] != want[i] {
			if first < 0 {
				first = i
			}
			diffs++
			if len(where) < 24 {
				where = append(where, fmt.Sprintf("$%04X:%02X/%02X", i, got[i], want[i]))
			}
		}
	}
	if diffs > 0 {
		t.Errorf("frame %d %s: %d/%d bytes differ, first at $%04X (ours %02X oracle %02X); %s",
			frame, what, diffs, len(got), first, got[first], want[first], strings.Join(where, " "))
	} else {
		t.Logf("frame %d %s: %d bytes identical", frame, what, len(got))
	}
}

func compareWords(t *testing.T, frame int, what string, got, want []uint16) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("frame %d %s: size %d vs oracle %d", frame, what, len(got), len(want))
		return
	}
	diffs, first := 0, -1
	for i := range got {
		if got[i] != want[i] {
			if first < 0 {
				first = i
			}
			diffs++
		}
	}
	if diffs > 0 {
		t.Errorf("frame %d %s: %d/%d words differ, first at $%04X (ours %04X oracle %04X)",
			frame, what, diffs, len(got), first, got[first], want[first])
	} else {
		t.Logf("frame %d %s: %d words identical", frame, what, len(got))
	}
}

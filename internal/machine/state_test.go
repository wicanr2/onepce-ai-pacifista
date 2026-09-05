package machine

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/wicanr2/onepce-ai-pacifista/internal/psg"
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

		// Every byte is compared, stack page included: with the VDC access
		// queue and DMA timing in place (spec vdc-vce.md §5.1) interrupts land
		// on the same instructions as in the oracle. ONEPCE_STATE_IGNORE (hex
		// offsets into work RAM, comma separated) is kept for other ROMs whose
		// timing counters are not yet accounted for; ignored bytes are reported.
		ram := readBin(t, filepath.Join(fixtures, fmt.Sprintf("ram-%d.bin", frame)))
		ignore := map[int]bool{}
		for _, h := range strings.Split(os.Getenv("ONEPCE_STATE_IGNORE"), ",") {
			if v, err := strconv.ParseInt(strings.TrimSpace(h), 16, 32); err == nil {
				ignore[int(v)] = true
			}
		}
		compareBytesIgnoring(t, frame, "work RAM", ramAtFrame[:], ram, ignore)

		vram := readWords(t, filepath.Join(fixtures, fmt.Sprintf("vram-%d.bin", frame)))
		compareWords(t, frame, "VRAM", m.VDC.VRAM(), vram)

		if p := filepath.Join(fixtures, fmt.Sprintf("sat-%d.bin", frame)); fileExists(p) {
			compareWords(t, frame, "SAT", m.VDC.SAT(), readWords(t, p))
		}
		pal := readWords(t, filepath.Join(fixtures, fmt.Sprintf("palette-%d.bin", frame)))
		compareWords(t, frame, "palette", m.VCE.Palette(), pal)
		comparePSG(t, frame, st, m.PSG.LastWriteState())
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
		t.Logf("frame %d %s: identical (%d bytes in the ignore set differ)", frame, what, ignored)
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

// comparePSG checks every psg.* key of the oracle's state dump against the
// chip as it was at its last port write (docs/spec/psg.md P14/§6).
func comparePSG(t *testing.T, frame int, st map[string]float64, ps psg.State) {
	t.Helper()
	b := func(v bool) int64 {
		if v {
			return 1
		}
		return 0
	}
	ours := map[string]int64{
		"psg.channelSelect": int64(ps.ChannelSelect), "psg.leftVolume": int64(ps.LeftVolume), "psg.rightVolume": int64(ps.RightVolume),
		"psg.lfoFrequency": int64(ps.LFOFrequency), "psg.lfoControl": int64(ps.LFOControl),
	}
	for i, c := range ps.Channels {
		k := fmt.Sprintf("psg.channels[%d].", i)
		ours[k+"amplitude"] = int64(c.Amplitude)
		ours[k+"currentOutput"] = int64(c.CurrentOutput)
		ours[k+"ddaEnabled"] = b(c.DDAEnabled)
		ours[k+"ddaOutputValue"] = int64(c.DDAOutputValue)
		ours[k+"enabled"] = b(c.Enabled)
		ours[k+"frequency"] = int64(c.Frequency)
		ours[k+"leftVolume"] = int64(c.LeftVolume)
		ours[k+"rightVolume"] = int64(c.RightVolume)
		ours[k+"noiseEnabled"] = b(c.NoiseEnabled)
		ours[k+"noiseFrequency"] = int64(c.NoiseFrequency)
		ours[k+"noiseLfsr"] = int64(c.NoiseLFSR)
		ours[k+"noiseOutput"] = int64(c.NoiseOutput)
		ours[k+"noiseTimer"] = int64(c.NoiseTimer)
		ours[k+"timer"] = int64(c.Timer)
		ours[k+"waveAddr"] = int64(c.WaveAddr)
		for w, v := range c.WaveData {
			ours[k+fmt.Sprintf("waveData%d", w)] = int64(v)
		}
	}
	compared, diffs := 0, []string{}
	for key, want := range st {
		if !strings.HasPrefix(key, "psg.") {
			continue
		}
		got, ok := ours[key]
		if !ok {
			continue
		}
		compared++
		if got != int64(want) {
			diffs = append(diffs, fmt.Sprintf("%s=%d/%v", key, got, want))
		}
	}
	if compared == 0 {
		t.Fatalf("frame %d: the oracle dump has no psg.* keys", frame)
	}
	if len(diffs) > 0 {
		sort.Strings(diffs)
		if len(diffs) > 20 {
			diffs = append(diffs[:20], fmt.Sprintf("… %d more", len(diffs)-20))
		}
		t.Errorf("frame %d PSG: %d of %d keys differ (ours/oracle): %s", frame, len(diffs), compared, strings.Join(diffs, " "))
	} else {
		t.Logf("frame %d PSG: %d keys identical", frame, compared)
	}
}

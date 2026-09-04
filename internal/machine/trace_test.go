package machine

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/wicanr2/onepce-ai-remake/internal/huc6280"
)

// Oracle comparison (docs/spec/huc6280.md §8, vdc-vce.md §10): run the whole
// machine on the real ROM and compare the PC+opcode sequence with the Mesen2
// boot trace. Inputs are private and live outside the repo:
//
//	ONEPCE_ROM      path to the .pce
//	ONEPCE_FIXTURES directory holding trace.tsv and samples.tsv
//
// Without them the test skips and says so — a skip is not a pass.
func TestMachineFollowsTheMesen2BootTrace(t *testing.T) {
	romPath := os.Getenv("ONEPCE_ROM")
	fixtures := os.Getenv("ONEPCE_FIXTURES")
	if romPath == "" || fixtures == "" {
		t.Skip("ONEPCE_ROM / ONEPCE_FIXTURES not set: oracle trace comparison skipped")
	}
	rom, err := os.ReadFile(romPath)
	if err != nil {
		t.Fatal(err)
	}
	trace, err := os.Open(filepath.Join(fixtures, "trace.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	defer trace.Close()

	m, err := New(rom)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("ROM sha256 %s, reset PC $%04X", m.Bus.ROMHash, m.CPU.PC)
	samples := loadSamples(t, filepath.Join(fixtures, "samples.tsv"))

	// ONEPCE_TRACE_LIMIT bounds the comparison (absolute instruction index);
	// the default covers the whole 200,000-instruction boot fixture. A fixture
	// recorded with TRACE_START skips that many instructions first (summary.txt).
	limit := 200000
	if v := os.Getenv("ONEPCE_TRACE_LIMIT"); v != "" {
		limit, _ = strconv.Atoi(v)
	}
	if plan := os.Getenv("ONEPCE_TRACE_PRESS"); plan != "" {
		presses, err := ParsePlan(plan)
		if err != nil {
			t.Fatal(err)
		}
		m.Schedule(presses...)
	}
	summary := readSummary(t, filepath.Join(fixtures, "summary.txt"))
	start, startFrame, total := summary["start"], summary["start_frame"], summary["instructions"]
	if startFrame > 0 {
		// Recording began after that frame: run there and require the same
		// instruction count, which is itself a parity check.
		n := 0
		for m.VDC.Frame() < uint64(startFrame) {
			m.Step()
			n++
		}
		if n != start {
			t.Fatalf("frame %d reached after %d instructions, oracle after %d", startFrame, n, start)
		}
	} else {
		for i := 0; i < start; i++ {
			m.Step()
		}
	}
	if start > 0 {
		t.Logf("skipped %d instructions to reach the fixture start (frame %d)", start, m.VDC.Frame())
	}
	if total > limit {
		total = limit
	}

	oracle := sha256.New()
	ours := sha256.New()
	scanner := bufio.NewScanner(trace)
	index := start
	var recent []string
	var baseOracle, baseOurs int64 = -1, 0
	// A fixture recorded with TRACE_LINES=0 has no per-instruction lines:
	// only the samples are compared, up to the recorded instruction count.
	hasLines := scanner.Scan()
	if !hasLines {
		t.Logf("fixture has no trace lines: comparing %d register/clock samples only", len(samples))
	}
	for index < limit && (hasLines || index < total) {
		snap := m.CPU.Peek()
		if hasLines {
			fields := strings.Split(scanner.Text(), "\t")
			if len(fields) != 2 {
				t.Fatalf("trace line %d: %q", index, scanner.Text())
			}
			wantPC, _ := strconv.ParseUint(fields[0], 16, 16)
			wantOp, _ := strconv.ParseUint(fields[1], 16, 8)
			fmt.Fprintf(oracle, "%04X%02X", wantPC, wantOp)
			fmt.Fprintf(ours, "%04X%02X", snap.PC, snap.Opcode)
			if uint16(wantPC) != snap.PC || uint8(wantOp) != snap.Opcode {
				regs := m.VDC.Registers()
				t.Fatalf("diverged at instruction %d (frame %d scanline %d hclock %d vmode %d rcr %d):\n  oracle PC=$%04X op=%02X\n  ours   %s op=%02X (%s) A=%02X X=%02X Y=%02X S=%02X P=%02X cycles=%d\n  recent: %s",
					index, regs.Frame, regs.Scanline, regs.HClock, regs.VMode, regs.RCRCount,
					wantPC, wantOp, m.Bus.Resolve(snap.PC), snap.Opcode, huc6280.Table[snap.Opcode].Name,
					snap.A, snap.X, snap.Y, snap.S, snap.P, snap.Cycles, strings.Join(recent, " "))
			}
		}
		if s, ok := samples[index]; ok {
			if pc, err := strconv.ParseUint(s.pc, 16, 16); err == nil && uint16(pc) != snap.PC {
				t.Fatalf("sample at instruction %d (frame %d): PC $%04X, oracle $%04X", index, m.VDC.Frame(), snap.PC, pc)
			}
			got := fmt.Sprintf("%02X %02X %02X %02X %02X", snap.A, snap.X, snap.Y, snap.S, snap.P)
			mpr := m.Bus.MPR()
			mprText := fmt.Sprintf("%02X %02X %02X %02X %02X %02X %02X %02X",
				mpr[0], mpr[1], mpr[2], mpr[3], mpr[4], mpr[5], mpr[6], mpr[7])
			if got != s.regs || mprText != s.mpr {
				t.Fatalf("sample at instruction %d: registers %q mpr %q, oracle %q %q", index, got, mprText, s.regs, s.mpr)
			}
			// Master-clock drift: both sides count from their own origin, so
			// compare the distance from the first sample (spec vdc-vce.md §5.1
			// acceptance). Any drift is a timing bug even before it moves an IRQ.
			if s.master >= 0 {
				ours := int64(m.Bus.MasterCycles())
				if baseOracle < 0 {
					baseOracle, baseOurs = s.master, ours
				} else if (s.master - baseOracle) != (ours - baseOurs) {
					t.Fatalf("master clock drift at instruction %d (frame %d): oracle +%d, ours +%d (difference %d clocks)",
						index, m.VDC.Frame(), s.master-baseOracle, ours-baseOurs, (ours-baseOurs)-(s.master-baseOracle))
				}
			}
		}
		recent = append(recent, fmt.Sprintf("%04X:%02X", snap.PC, snap.Opcode))
		if len(recent) > 12 {
			recent = recent[1:]
		}
		m.Step()
		index++
		if hasLines && index < limit && !scanner.Scan() {
			break
		}
	}
	if index == start {
		t.Fatal("trace fixture is empty: nothing was compared")
	}
	o, u := hex.EncodeToString(oracle.Sum(nil)), hex.EncodeToString(ours.Sum(nil))
	if o != u {
		t.Fatalf("structure hash differs after %d instructions", index)
	}
	t.Logf("%d instructions, %d register samples, %d frames, structure sha256 %s", index-start, len(samples), m.VDC.Frame(), o)
}

// readSummary parses the probe's summary.txt ("key=int" lines); missing
// keys read as 0, a missing file as empty.
func readSummary(t *testing.T, path string) map[string]int {
	t.Helper()
	out := map[string]int{}
	b, err := os.ReadFile(path)
	if err != nil {
		return out
	}
	for _, line := range strings.Split(string(b), "\n") {
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		n, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil {
			t.Fatalf("%s: %q: %v", path, line, err)
		}
		out[k] = n
	}
	return out
}

type sample struct {
	pc        string
	regs, mpr string
	master    int64 // Mesen2 masterClock at the sample, -1 when the fixture predates the column
}

func loadSamples(t *testing.T, path string) map[int]sample {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	out := map[int]sample{}
	scanner := bufio.NewScanner(f)
	scanner.Scan() // header
	for scanner.Scan() {
		fields := strings.Split(scanner.Text(), "\t")
		if len(fields) < 9 {
			continue
		}
		index, _ := strconv.Atoi(fields[0])
		sm := sample{pc: fields[1], regs: strings.Join(fields[2:7], " "), mpr: fields[7], master: -1}
		if len(fields) >= 10 {
			if v, err := strconv.ParseInt(fields[9], 10, 64); err == nil {
				sm.master = v
			}
		}
		out[index] = sm
	}
	return out
}

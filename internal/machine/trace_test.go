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

	// The fixture runs past the point where the game enables interrupts. From
	// there on the timer IRQ lands on whichever instruction the accumulated
	// cycle count puts it on, and the cycle count differs because VRAM write
	// stalls are not modelled (docs/spec/vdc-vce.md §8). The comparison is
	// therefore bounded; the bound is a fixture fact, not a target.
	limit := 160000
	if v := os.Getenv("ONEPCE_TRACE_LIMIT"); v != "" {
		limit, _ = strconv.Atoi(v)
	}

	oracle := sha256.New()
	ours := sha256.New()
	scanner := bufio.NewScanner(trace)
	index := 0
	var recent []string
	for scanner.Scan() && index < limit {
		fields := strings.Split(scanner.Text(), "\t")
		if len(fields) != 2 {
			t.Fatalf("trace line %d: %q", index, scanner.Text())
		}
		wantPC, _ := strconv.ParseUint(fields[0], 16, 16)
		wantOp, _ := strconv.ParseUint(fields[1], 16, 8)
		snap := m.CPU.Peek()
		fmt.Fprintf(oracle, "%04X%02X", wantPC, wantOp)
		fmt.Fprintf(ours, "%04X%02X", snap.PC, snap.Opcode)
		if uint16(wantPC) != snap.PC || uint8(wantOp) != snap.Opcode {
			regs := m.VDC.Registers()
			t.Fatalf("diverged at instruction %d (frame %d scanline %d hclock %d vmode %d rcr %d):\n  oracle PC=$%04X op=%02X\n  ours   %s op=%02X (%s) A=%02X X=%02X Y=%02X S=%02X P=%02X cycles=%d\n  recent: %s",
				index, regs.Frame, regs.Scanline, regs.HClock, regs.VMode, regs.RCRCount,
				wantPC, wantOp, m.Bus.Resolve(snap.PC), snap.Opcode, huc6280.Table[snap.Opcode].Name,
				snap.A, snap.X, snap.Y, snap.S, snap.P, snap.Cycles, strings.Join(recent, " "))
		}
		if s, ok := samples[index]; ok {
			got := fmt.Sprintf("%02X %02X %02X %02X %02X", snap.A, snap.X, snap.Y, snap.S, snap.P)
			mpr := m.Bus.MPR()
			mprText := fmt.Sprintf("%02X %02X %02X %02X %02X %02X %02X %02X",
				mpr[0], mpr[1], mpr[2], mpr[3], mpr[4], mpr[5], mpr[6], mpr[7])
			if got != s.regs || mprText != s.mpr {
				t.Fatalf("sample at instruction %d: registers %q mpr %q, oracle %q %q", index, got, mprText, s.regs, s.mpr)
			}
		}
		recent = append(recent, fmt.Sprintf("%04X:%02X", snap.PC, snap.Opcode))
		if len(recent) > 12 {
			recent = recent[1:]
		}
		m.Step()
		index++
	}
	if index == 0 {
		t.Fatal("trace fixture is empty: nothing was compared")
	}
	o, u := hex.EncodeToString(oracle.Sum(nil)), hex.EncodeToString(ours.Sum(nil))
	if o != u {
		t.Fatalf("structure hash differs after %d instructions", index)
	}
	t.Logf("%d instructions, %d register samples, %d frames, structure sha256 %s", index, len(samples), m.VDC.Frame(), o)
}

type sample struct{ regs, mpr string }

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
		out[index] = sample{regs: strings.Join(fields[2:7], " "), mpr: fields[7]}
	}
	return out
}

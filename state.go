package onepce

import (
	"bufio"
	"encoding/gob"
	"encoding/json"
	"fmt"
	"io"

	"github.com/wicanr2/onepce-ai-remake/internal/machine"
)

// StateFormat is the savestate container version (docs/spec/state.md S2).
const StateFormat = 3

// stateHeader is the JSON line that starts a savestate (spec S1).
type stateHeader struct {
	Format   int     `json:"format"`
	Emulator string  `json:"emulator"`
	ROMHash  string  `json:"rom_sha256"`
	Frame    uint64  `json:"frame"`
	Plan     []Press `json:"plan"`
}

// SaveState writes the whole machine: one JSON header line, then the gob
// encoded component states.
func (pm *Machine) SaveState(w io.Writer) error {
	bw := bufio.NewWriter(w)
	head, err := json.Marshal(stateHeader{Format: StateFormat, Emulator: Version, ROMHash: pm.romHash,
		Frame: pm.Frame(), Plan: pm.Plan()})
	if err != nil {
		return err
	}
	if _, err := bw.Write(append(head, '\n')); err != nil {
		return err
	}
	if err := gob.NewEncoder(bw).Encode(pm.m.Save()); err != nil {
		return fmt.Errorf("savestate: %w", err)
	}
	return bw.Flush()
}

// LoadState restores a savestate into this machine. The format version and
// the ROM hash must match (spec S2/S3); the input plan recorded in the file
// replaces the current one.
func (pm *Machine) LoadState(r io.Reader) error {
	br := bufio.NewReader(r)
	line, err := br.ReadBytes('\n')
	if err != nil {
		return fmt.Errorf("savestate: header: %w", err)
	}
	var head stateHeader
	if err := json.Unmarshal(line, &head); err != nil {
		return fmt.Errorf("savestate: header: %w", err)
	}
	if head.Format != StateFormat {
		return fmt.Errorf("savestate: format %d, this build reads %d", head.Format, StateFormat)
	}
	if head.ROMHash != pm.romHash {
		return fmt.Errorf("savestate: ROM sha256 %s does not match loaded %s", head.ROMHash, pm.romHash)
	}
	var s machine.State
	if err := gob.NewDecoder(br).Decode(&s); err != nil {
		return fmt.Errorf("savestate: body: %w", err)
	}
	pm.m.Restore(s)
	pm.plan = append([]Press(nil), head.Plan...)
	return nil
}

package onepce

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/gob"
	"encoding/hex"

	"github.com/wicanr2/onepce-ai-pacifista/internal/huc6280"
	"github.com/wicanr2/onepce-ai-pacifista/internal/psg"
	"github.com/wicanr2/onepce-ai-pacifista/internal/vdc"
)

// Section names a snapshot region (docs/spec/observe.md O7).
type Section string

const (
	SectionRAM     Section = "RAM"
	SectionVRAM    Section = "VRAM"
	SectionSAT     Section = "SAT"
	SectionVCE     Section = "VCE"
	SectionVDCRegs Section = "VDCRegs"
	SectionCPU     Section = "CPU"
	SectionPSG     Section = "PSG"
)

// AllSections is every section, in a fixed order.
var AllSections = []Section{SectionRAM, SectionVRAM, SectionSAT, SectionVCE, SectionVDCRegs, SectionCPU, SectionPSG}

// Snapshot is a labelled copy of machine state with provenance.
type Snapshot struct {
	Version string
	ROMHash string
	Frame   uint64
	Plan    []Press

	CPU     huc6280.Snapshot
	MPR     MPR
	RAM     []byte   // 8 KiB work RAM
	VRAM    []uint16 // 32K words
	SAT     []uint16 // 256 words
	VCE     []uint16 // 512 palette entries
	VDCRegs vdc.Registers
	PSG     psg.State

	// Hashes holds the SHA-256 of every section that was captured, keyed by
	// section name, so two snapshots can be compared without the payload.
	Hashes map[Section]string
}

// Snapshot captures the requested sections (all of them when none given).
func (pm *Machine) Snapshot(sections ...Section) *Snapshot {
	if len(sections) == 0 {
		sections = AllSections
	}
	s := &Snapshot{Version: Version, ROMHash: pm.romHash, Frame: pm.Frame(), Plan: pm.Plan(),
		Hashes: map[Section]string{}}
	for _, sec := range sections {
		switch sec {
		case SectionRAM:
			s.RAM = append([]byte(nil), pm.m.Bus.RAM[:]...)
			s.Hashes[sec] = hashBytes(s.RAM)
		case SectionVRAM:
			s.VRAM = pm.m.VDC.VRAM()
			s.Hashes[sec] = hashWords(s.VRAM)
		case SectionSAT:
			s.SAT = pm.m.VDC.SAT()
			s.Hashes[sec] = hashWords(s.SAT)
		case SectionVCE:
			s.VCE = pm.m.VCE.Palette()
			s.Hashes[sec] = hashWords(s.VCE)
		case SectionVDCRegs:
			s.VDCRegs = pm.m.VDC.Registers()
			s.Hashes[sec] = hashWords(s.VDCRegs.Raw[:])
		case SectionPSG:
			s.PSG = pm.m.PSG.State
			var buf bytes.Buffer
			_ = gob.NewEncoder(&buf).Encode(s.PSG)
			s.Hashes[sec] = hashBytes(buf.Bytes())
		case SectionCPU:
			s.CPU = pm.m.CPU.Peek()
			s.MPR = pm.m.Bus.MPR()
			buf := []byte{s.CPU.A, s.CPU.X, s.CPU.Y, s.CPU.S, s.CPU.P, uint8(s.CPU.PC), uint8(s.CPU.PC >> 8)}
			buf = append(buf, s.MPR[:]...)
			s.Hashes[sec] = hashBytes(buf)
		}
	}
	return s
}

func hashBytes(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func hashWords(w []uint16) string {
	b := make([]byte, 2*len(w))
	for i, v := range w {
		binary.LittleEndian.PutUint16(b[2*i:], v)
	}
	return hashBytes(b)
}

// Diff lists the sections whose hashes differ between two snapshots.
func (s *Snapshot) Diff(o *Snapshot) []Section {
	var out []Section
	for _, sec := range AllSections {
		a, okA := s.Hashes[sec]
		b, okB := o.Hashes[sec]
		if okA && okB && a != b {
			out = append(out, sec)
		}
	}
	return out
}

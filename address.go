// Package onepce is the public entry point of the OnePCE AI Remake emulator:
// a headless-first PC Engine emulator whose state is fully observable, so that
// AI agents and remake test suites can query the original game as a library.
//
// Only four concepts leak out of this package: Machine, Snapshot, Event and
// Input (docs/PLAN.md §五). Everything else is internal.
package onepce

import "fmt"

// MPR holds the eight memory paging registers of the HuC6280. Page i of the
// 64 KiB logical space (logical>>13) is backed by the 8 KiB physical bank
// MPR[i]. Spec: docs/spec/address-model.md (A1–A3).
type MPR [8]uint8

// Physical maps a CPU logical address through the paging registers to the
// 21-bit physical bus address.
func (m MPR) Physical(logical uint16) uint32 {
	return uint32(m[logical>>13])<<13 | uint32(logical&0x1FFF)
}

// Bank is the physical 8 KiB bank number the logical address lands in.
func (m MPR) Bank(logical uint16) uint8 {
	return m[logical>>13]
}

// FileUnknown marks an address with no ROM file offset (RAM, I/O, unmapped).
const FileUnknown int64 = -1

// Address is one location seen from all three address spaces at once, with
// the paging state that made the translation. An address without its MPR is
// meaningless (docs/spec/address-model.md A3), which is why there is no
// constructor from a bare logical value.
type Address struct {
	Logical  uint16
	Physical uint32
	File     int64 // ROM file offset, or FileUnknown
	MPR      MPR
}

// Resolve builds an Address for a logical location under the given paging
// state. The file offset is filled in by the caller (the bus knows the mapper;
// this package does not), so it starts as FileUnknown.
func Resolve(logical uint16, mpr MPR) Address {
	return Address{Logical: logical, Physical: mpr.Physical(logical), File: FileUnknown, MPR: mpr}
}

// String renders the fixed display format shared by every trace, event and
// snapshot: L:$6151 P:$28151 F:0x28151 MPR=[FF F8 13 14 01 02 03 00].
func (a Address) String() string {
	file := "unknown"
	if a.File >= 0 {
		file = fmt.Sprintf("0x%05X", a.File)
	}
	return fmt.Sprintf("L:$%04X P:$%05X F:%s MPR=[%02X %02X %02X %02X %02X %02X %02X %02X]",
		a.Logical, a.Physical, file,
		a.MPR[0], a.MPR[1], a.MPR[2], a.MPR[3], a.MPR[4], a.MPR[5], a.MPR[6], a.MPR[7])
}

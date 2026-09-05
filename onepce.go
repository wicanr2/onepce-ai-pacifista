// Package onepce is the public entry point of the OnePCE AI Remake emulator:
// a headless-first PC Engine emulator whose state is fully observable, so that
// AI agents and remake test suites can query the original game as a library.
//
// Only four concepts leak out of this package: Machine, Snapshot, Event and
// Press (docs/PLAN.md §五). Everything else is internal.
package onepce

import "github.com/wicanr2/onepce-ai-pacifista/internal/addr"

// Address model (docs/spec/address-model.md), re-exported from internal/addr.
type (
	MPR     = addr.MPR
	Address = addr.Address
)

// FileUnknown marks an address with no ROM file offset (RAM, I/O, unmapped).
const FileUnknown = addr.FileUnknown

// Resolve builds an Address for a logical location under the given paging
// state; the file offset starts unknown until a bus fills it in.
func Resolve(logical uint16, mpr MPR) Address { return addr.Resolve(logical, mpr) }

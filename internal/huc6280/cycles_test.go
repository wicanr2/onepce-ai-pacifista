package huc6280

import "testing"

// Every opcode is executed once on the synthetic bus and the cycles counted
// from bus accesses and idles must equal the table's base count (spec §6).
// The table came from the public timing documentation; the access placement
// came from behaviour facts. Disagreement means one of them is wrong, and the
// difference must be settled by the oracle, not by editing whichever is
// easier.
func TestCountedCyclesMatchTheTable(t *testing.T) {
	for opcode := 0; opcode < 256; opcode++ {
		o := Table[opcode]
		code := []uint8{uint8(opcode), 0x10, 0x20, 0x00, 0x30, 0x00, 0x00}
		c, b := newCPU(t, code...)
		// A zero displacement keeps taken branches in place; the "taken"
		// extra is checked separately below.
		if o.Mode == Rel || o.Mode == ZpRel {
			b.mem[0x8001], b.mem[0x8002] = 0x00, 0x00
		}
		if o.Mode == Blk {
			// TII $2010,$2020,#1: one byte, so 17 + 6.
			copy(b.mem[0x8001:], []uint8{0x10, 0x20, 0x20, 0x20, 0x01, 0x00})
		}
		c.P &^= FlagZ | FlagN | FlagV | FlagC // untaken for the plain branches on bit tests
		b.ticks = 0
		got := c.Step()
		want := int(o.Cycles)
		switch o.Name {
		case "BPL", "BVC", "BCC", "BNE":
			want += 2 // flags clear → these are taken
		case "BBR0", "BBR1", "BBR2", "BBR3", "BBR4", "BBR5", "BBR6", "BBR7":
			want += 2 // zero page byte is 0 → bit clear → taken
		}
		if o.Mode == Blk {
			want += 6
		}
		if got != want || b.ticks != got {
			t.Errorf("$%02X %s: counted %d cycles (bus saw %d), table says %d", opcode, o.Name, got, b.ticks, want)
		}
	}
}

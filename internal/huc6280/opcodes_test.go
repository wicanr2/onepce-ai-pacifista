package huc6280

import "testing"

// Every slot must be filled and its length must match its mode: a hole here
// would decode as a 1-byte NOP and silently desynchronise everything after it.
func TestTableIsCompleteAndSelfConsistent(t *testing.T) {
	for i, o := range Table {
		if o.Name == "" {
			t.Errorf("opcode $%02X has no mnemonic", i)
		}
		if o.Length != modeLength[o.Mode] {
			t.Errorf("opcode $%02X %s: length %d does not match mode %d", i, o.Name, o.Length, o.Mode)
		}
		if o.Cycles < 2 {
			t.Errorf("opcode $%02X %s: %d cycles is below the 2-cycle floor", i, o.Name, o.Cycles)
		}
	}
}

// Spot checks against the public instruction set (spec §4).
func TestTableSpotChecks(t *testing.T) {
	cases := []struct {
		opcode uint8
		name   string
		mode   Mode
		cycles uint8
	}{
		{0x73, "TII", Blk, 17},
		{0xF3, "TAI", Blk, 17},
		{0x53, "TAM", Imm, 5},
		{0x43, "TMA", Imm, 4},
		{0x03, "ST0", Imm, 4},
		{0x83, "TST", ImmZp, 7},
		{0xB3, "TST", ImmAbsX, 8},
		{0x7C, "JMP", AbsXInd, 7},
		{0x0F, "BBR0", ZpRel, 6},
		{0xFF, "BBS7", ZpRel, 6},
		{0x44, "BSR", Rel, 8},
		{0xF4, "SET", Imp, 2},
		{0xB2, "LDA", ZpInd, 7},
		{0x9E, "STZ", AbsX, 5},
	}
	for _, c := range cases {
		o := Table[c.opcode]
		if o.Name != c.name || o.Mode != c.mode || o.Cycles != c.cycles {
			t.Errorf("$%02X = %s/%d/%d cycles, want %s/%d/%d", c.opcode, o.Name, o.Mode, o.Cycles, c.name, c.mode, c.cycles)
		}
	}
	// Block transfers are the only 7-byte instructions.
	for i, o := range Table {
		if (o.Length == 7) != (o.Mode == Blk) {
			t.Errorf("$%02X: 7-byte length and Blk mode must go together", i)
		}
	}
}

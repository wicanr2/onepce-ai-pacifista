package huc6280

// The 256-entry opcode table. Spec: docs/spec/huc6280.md §3–§4, §6.
//
// Source of the mnemonic/mode assignment: the public HuC6280 instruction set
// (65C02 core plus Hudson's extensions). Base cycle counts come from the public
// timing tables; each row was cross-checked once against Mesen2's per-access
// cycle accounting (b9fa69d) and the differences are recorded in the spec's
// errata rather than silently adopted. The table is this project's own data,
// in this project's own format.

// Mode is an addressing mode as named in docs/spec/huc6280.md §3.
type Mode uint8

const (
	Imp Mode = iota
	Acc
	Imm
	Zp
	ZpX
	ZpY
	Abs
	AbsX
	AbsY
	Ind      // JMP (abs)
	AbsXInd  // JMP (abs,X)
	IndX     // (zp,X)
	IndY     // (zp),Y
	ZpInd    // (zp)
	Rel      // branch
	ZpRel    // BBRn/BBSn: zp, rel
	ImmZp    // TST #imm, zp
	ImmZpX   // TST #imm, zp,X
	ImmAbs   // TST #imm, abs
	ImmAbsX  // TST #imm, abs,X
	Blk      // TII/TDD/TIN/TIA/TAI: src, dst, len
	modeLast // sentinel
)

// modeLength is the instruction length in bytes for each mode.
var modeLength = [modeLast]uint8{
	Imp: 1, Acc: 1, Imm: 2, Zp: 2, ZpX: 2, ZpY: 2, Abs: 3, AbsX: 3, AbsY: 3,
	Ind: 3, AbsXInd: 3, IndX: 2, IndY: 2, ZpInd: 2, Rel: 2, ZpRel: 3,
	ImmZp: 3, ImmZpX: 3, ImmAbs: 4, ImmAbsX: 4, Blk: 7,
}

// Op is one table entry.
type Op struct {
	Name   string
	Mode   Mode
	Length uint8
	Cycles uint8 // base cycles; branches add 2 when taken, block transfers add 6 per byte
}

func op(name string, mode Mode, cycles uint8) Op {
	return Op{Name: name, Mode: mode, Length: modeLength[mode], Cycles: cycles}
}

// Table is indexed by opcode. Undefined opcodes are 1-byte, 2-cycle NOPs.
var Table = [256]Op{
	// 0x00
	op("BRK", Imm, 8), op("ORA", IndX, 7), op("SXY", Imp, 3), op("ST0", Imm, 4),
	op("TSB", Zp, 6), op("ORA", Zp, 4), op("ASL", Zp, 6), op("RMB0", Zp, 7),
	op("PHP", Imp, 3), op("ORA", Imm, 2), op("ASL", Acc, 2), op("NOP", Imp, 2),
	op("TSB", Abs, 7), op("ORA", Abs, 5), op("ASL", Abs, 7), op("BBR0", ZpRel, 6),
	// 0x10
	op("BPL", Rel, 2), op("ORA", IndY, 7), op("ORA", ZpInd, 7), op("ST1", Imm, 4),
	op("TRB", Zp, 6), op("ORA", ZpX, 4), op("ASL", ZpX, 6), op("RMB1", Zp, 7),
	op("CLC", Imp, 2), op("ORA", AbsY, 5), op("INC", Acc, 2), op("NOP", Imp, 2),
	op("TRB", Abs, 7), op("ORA", AbsX, 5), op("ASL", AbsX, 7), op("BBR1", ZpRel, 6),
	// 0x20
	op("JSR", Abs, 7), op("AND", IndX, 7), op("SAX", Imp, 3), op("ST2", Imm, 4),
	op("BIT", Zp, 4), op("AND", Zp, 4), op("ROL", Zp, 6), op("RMB2", Zp, 7),
	op("PLP", Imp, 4), op("AND", Imm, 2), op("ROL", Acc, 2), op("NOP", Imp, 2),
	op("BIT", Abs, 5), op("AND", Abs, 5), op("ROL", Abs, 7), op("BBR2", ZpRel, 6),
	// 0x30
	op("BMI", Rel, 2), op("AND", IndY, 7), op("AND", ZpInd, 7), op("NOP", Imp, 2),
	op("BIT", ZpX, 4), op("AND", ZpX, 4), op("ROL", ZpX, 6), op("RMB3", Zp, 7),
	op("SEC", Imp, 2), op("AND", AbsY, 5), op("DEC", Acc, 2), op("NOP", Imp, 2),
	op("BIT", AbsX, 5), op("AND", AbsX, 5), op("ROL", AbsX, 7), op("BBR3", ZpRel, 6),
	// 0x40
	op("RTI", Imp, 7), op("EOR", IndX, 7), op("SAY", Imp, 3), op("TMA", Imm, 4),
	op("BSR", Rel, 8), op("EOR", Zp, 4), op("LSR", Zp, 6), op("RMB4", Zp, 7),
	op("PHA", Imp, 3), op("EOR", Imm, 2), op("LSR", Acc, 2), op("NOP", Imp, 2),
	op("JMP", Abs, 4), op("EOR", Abs, 5), op("LSR", Abs, 7), op("BBR4", ZpRel, 6),
	// 0x50
	op("BVC", Rel, 2), op("EOR", IndY, 7), op("EOR", ZpInd, 7), op("TAM", Imm, 5),
	op("CSL", Imp, 3), op("EOR", ZpX, 4), op("LSR", ZpX, 6), op("RMB5", Zp, 7),
	op("CLI", Imp, 2), op("EOR", AbsY, 5), op("PHY", Imp, 3), op("NOP", Imp, 2),
	op("NOP", Imp, 2), op("EOR", AbsX, 5), op("LSR", AbsX, 7), op("BBR5", ZpRel, 6),
	// 0x60
	op("RTS", Imp, 7), op("ADC", IndX, 7), op("CLA", Imp, 2), op("NOP", Imp, 2),
	op("STZ", Zp, 4), op("ADC", Zp, 4), op("ROR", Zp, 6), op("RMB6", Zp, 7),
	op("PLA", Imp, 4), op("ADC", Imm, 2), op("ROR", Acc, 2), op("NOP", Imp, 2),
	op("JMP", Ind, 7), op("ADC", Abs, 5), op("ROR", Abs, 7), op("BBR6", ZpRel, 6),
	// 0x70
	op("BVS", Rel, 2), op("ADC", IndY, 7), op("ADC", ZpInd, 7), op("TII", Blk, 17),
	op("STZ", ZpX, 4), op("ADC", ZpX, 4), op("ROR", ZpX, 6), op("RMB7", Zp, 7),
	op("SEI", Imp, 2), op("ADC", AbsY, 5), op("PLY", Imp, 4), op("NOP", Imp, 2),
	op("JMP", AbsXInd, 7), op("ADC", AbsX, 5), op("ROR", AbsX, 7), op("BBR7", ZpRel, 6),
	// 0x80
	op("BRA", Rel, 4), op("STA", IndX, 7), op("CLX", Imp, 2), op("TST", ImmZp, 7),
	op("STY", Zp, 4), op("STA", Zp, 4), op("STX", Zp, 4), op("SMB0", Zp, 7),
	op("DEY", Imp, 2), op("BIT", Imm, 2), op("TXA", Imp, 2), op("NOP", Imp, 2),
	op("STY", Abs, 5), op("STA", Abs, 5), op("STX", Abs, 5), op("BBS0", ZpRel, 6),
	// 0x90
	op("BCC", Rel, 2), op("STA", IndY, 7), op("STA", ZpInd, 7), op("TST", ImmAbs, 8),
	op("STY", ZpX, 4), op("STA", ZpX, 4), op("STX", ZpY, 4), op("SMB1", Zp, 7),
	op("TYA", Imp, 2), op("STA", AbsY, 5), op("TXS", Imp, 2), op("NOP", Imp, 2),
	op("STZ", Abs, 5), op("STA", AbsX, 5), op("STZ", AbsX, 5), op("BBS1", ZpRel, 6),
	// 0xA0
	op("LDY", Imm, 2), op("LDA", IndX, 7), op("LDX", Imm, 2), op("TST", ImmZpX, 7),
	op("LDY", Zp, 4), op("LDA", Zp, 4), op("LDX", Zp, 4), op("SMB2", Zp, 7),
	op("TAY", Imp, 2), op("LDA", Imm, 2), op("TAX", Imp, 2), op("NOP", Imp, 2),
	op("LDY", Abs, 5), op("LDA", Abs, 5), op("LDX", Abs, 5), op("BBS2", ZpRel, 6),
	// 0xB0
	op("BCS", Rel, 2), op("LDA", IndY, 7), op("LDA", ZpInd, 7), op("TST", ImmAbsX, 8),
	op("LDY", ZpX, 4), op("LDA", ZpX, 4), op("LDX", ZpY, 4), op("SMB3", Zp, 7),
	op("CLV", Imp, 2), op("LDA", AbsY, 5), op("TSX", Imp, 2), op("NOP", Imp, 2),
	op("LDY", AbsX, 5), op("LDA", AbsX, 5), op("LDX", AbsY, 5), op("BBS3", ZpRel, 6),
	// 0xC0
	op("CPY", Imm, 2), op("CMP", IndX, 7), op("CLY", Imp, 2), op("TDD", Blk, 17),
	op("CPY", Zp, 4), op("CMP", Zp, 4), op("DEC", Zp, 6), op("SMB4", Zp, 7),
	op("INY", Imp, 2), op("CMP", Imm, 2), op("DEX", Imp, 2), op("NOP", Imp, 2),
	op("CPY", Abs, 5), op("CMP", Abs, 5), op("DEC", Abs, 7), op("BBS4", ZpRel, 6),
	// 0xD0
	op("BNE", Rel, 2), op("CMP", IndY, 7), op("CMP", ZpInd, 7), op("TIN", Blk, 17),
	op("CSH", Imp, 3), op("CMP", ZpX, 4), op("DEC", ZpX, 6), op("SMB5", Zp, 7),
	op("CLD", Imp, 2), op("CMP", AbsY, 5), op("PHX", Imp, 3), op("NOP", Imp, 2),
	op("NOP", Imp, 2), op("CMP", AbsX, 5), op("DEC", AbsX, 7), op("BBS5", ZpRel, 6),
	// 0xE0
	op("CPX", Imm, 2), op("SBC", IndX, 7), op("NOP", Imp, 2), op("TIA", Blk, 17),
	op("CPX", Zp, 4), op("SBC", Zp, 4), op("INC", Zp, 6), op("SMB6", Zp, 7),
	op("INX", Imp, 2), op("SBC", Imm, 2), op("NOP", Imp, 2), op("NOP", Imp, 2),
	op("CPX", Abs, 5), op("SBC", Abs, 5), op("INC", Abs, 7), op("BBS6", ZpRel, 6),
	// 0xF0
	op("BEQ", Rel, 2), op("SBC", IndY, 7), op("SBC", ZpInd, 7), op("TAI", Blk, 17),
	op("SET", Imp, 2), op("SBC", ZpX, 4), op("INC", ZpX, 6), op("SMB7", Zp, 7),
	op("SED", Imp, 2), op("SBC", AbsY, 5), op("PLX", Imp, 4), op("NOP", Imp, 2),
	op("NOP", Imp, 2), op("SBC", AbsX, 5), op("INC", AbsX, 7), op("BBS7", ZpRel, 6),
}

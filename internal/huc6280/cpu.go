// Package huc6280 is the CPU core: a 65C02-derived processor with Hudson's
// extensions (MPR paging, T flag, block transfers, VDC port stores).
//
// Spec: docs/spec/huc6280.md. 參考行為：Mesen2 PceCpu.cpp／PceCpu.Instructions.cpp
// @ b9fa69d §T flag、§block transfer、§interrupt priority（只取行為事實，結構是本專案的）。
package huc6280

// Flag bits of P (spec §1).
const (
	FlagC uint8 = 0x01
	FlagZ uint8 = 0x02
	FlagI uint8 = 0x04
	FlagD uint8 = 0x08
	FlagB uint8 = 0x10
	FlagT uint8 = 0x20
	FlagV uint8 = 0x40
	FlagN uint8 = 0x80
)

// Fixed logical locations (spec §2).
const (
	ZeroPage  uint16 = 0x2000
	StackPage uint16 = 0x2100
	VecIRQ2   uint16 = 0xFFF6 // also BRK
	VecIRQ1   uint16 = 0xFFF8
	VecTimer  uint16 = 0xFFFA
	VecNMI    uint16 = 0xFFFC
	VecReset  uint16 = 0xFFFE
	IRQ2      uint8  = 0x01
	IRQ1      uint8  = 0x02
	IRQTimer  uint8  = 0x04
)

// Bus is everything the CPU touches outside its registers. The bus owns the
// paging registers because it is the one that needs them to map an address.
type Bus interface {
	Read(logical uint16) uint8
	Write(logical uint16, value uint8)
	// Peek reads without clocking anything or touching I/O side effects.
	Peek(logical uint16) uint8
	// WriteVDCPort is ST0/ST1/ST2: the VDC port (0, 2 or 3) bypasses paging.
	WriteVDCPort(port uint8, value uint8)
	// SetMPR writes value into every MPR whose bit is set in mask (TAM).
	SetMPR(mask uint8, value uint8)
	// GetMPR ORs together the MPRs selected by mask; mask 0 returns the last
	// value written by SetMPR (TMA).
	GetMPR(mask uint8) uint8
	// SetSpeed is CSH (fast=true) / CSL.
	SetSpeed(fast bool)
	// Idle advances the clock by one CPU cycle with no bus access. Read and
	// Write each take one cycle of their own, so the bus is clocked by every
	// access the CPU makes — which is what lets the video side be observed
	// mid-instruction (spec §6).
	Idle()
	// PendingIRQ returns the interrupt lines that are both asserted and not
	// masked by $1402, as IRQ2|IRQ1|IRQTimer bits.
	PendingIRQ() uint8
	// SetIRQSampler installs the CPU's per-cycle interrupt sample. The bus
	// calls it on every CPU cycle after advancing the clock and before the
	// access (spec C4): an interrupt raised by the clock tick of a cycle is
	// seen in that cycle; one raised by the access itself is not.
	SetIRQSampler(func())
}

// CPU is the register file plus the cycle counter. Zero value is not usable;
// call New then Reset.
type CPU struct {
	A, X, Y, S, P uint8
	PC            uint16
	Cycles        uint64
	// InstPC is the address the current (or last) instruction started at:
	// what an event inside the instruction reports as its PC (spec O1).
	InstPC uint16

	bus     Bus
	n       int  // cycles spent so far in the current instruction
	memFlag bool // T flag sampled for the instruction being executed
	// irqSample is the interrupt request seen on the last bus cycle: the
	// lines pending at that cycle, or 0 if I was set at that cycle. Sampling
	// per cycle rather than after the instruction is what delays an
	// interrupt by one instruction after CLI/PLP (spec C4).
	irqSample uint8
	// Operand state for the current instruction.
	addr   uint16 // effective address for memory modes
	imm    uint8  // immediate value (Imm, TST, ST0-2, TAM/TMA)
	rel    int8   // branch displacement
	blkSrc uint16
	blkDst uint16
	blkLen uint16
	opcode uint8
	op     Op
}

// New wires a CPU to its bus. Registers are left for Reset.
func New(bus Bus) *CPU {
	c := &CPU{bus: bus}
	bus.SetIRQSampler(c.sampleIRQ)
	return c
}

// Reset applies the power-on state of spec §1: I set, D and T clear, PC from
// the reset vector. A, X, Y and S are hardware-undefined; this project fixes
// them to 0 (a remake decision, not a hardware fact).
func (c *CPU) Reset() {
	c.A, c.X, c.Y, c.S = 0, 0, 0, 0
	c.P = FlagI
	c.PC = uint16(c.bus.Peek(VecReset)) | uint16(c.bus.Peek(VecReset+1))<<8
	c.Cycles = 0
}

// Snapshot is the register view exposed to trace and watch hooks.
type Snapshot struct {
	PC            uint16
	Opcode        uint8
	A, X, Y, S, P uint8
	Cycles        uint64
}

// Peek returns the registers as they are right now; Opcode is the byte at PC.
func (c *CPU) Peek() Snapshot {
	return Snapshot{PC: c.PC, Opcode: c.bus.Peek(c.PC), A: c.A, X: c.X, Y: c.Y, S: c.S, P: c.P, Cycles: c.Cycles}
}

// Step executes one instruction, ticks the bus, then services a pending
// interrupt if I is clear (spec C3/C4). It returns the cycles consumed,
// interrupt entry included.
func (c *CPU) Step() int {
	c.memFlag = c.P&FlagT != 0
	c.P &^= FlagT

	c.n = 0
	c.InstPC = c.PC
	c.opcode = c.read(c.PC)
	c.PC++
	c.op = Table[c.opcode]
	c.fetchOperand()
	c.execute()
	cycles := c.n
	c.Cycles += uint64(cycles)

	pending := c.irqSample
	if pending&IRQTimer != 0 && c.bus.PendingIRQ()&IRQTimer == 0 {
		// The timer line is re-checked after the instruction: an acknowledge
		// in the instruction that saw it pending cancels it (spec C3).
		pending &^= IRQTimer
	}
	if pending != 0 {
		cycles += c.interrupt(pending)
	}
	return cycles
}

// sampleIRQ records what an interrupt check would see on this cycle.
func (c *CPU) sampleIRQ() {
	if c.P&FlagI != 0 {
		c.irqSample = 0
		return
	}
	c.irqSample = c.bus.PendingIRQ()
}

// Cycle-counted bus helpers. Every access is one cycle; idle adds internal
// cycles. The opcode table's counts are the cross-check (opcodes_test.go).
func (c *CPU) read(addr uint16) uint8 {
	c.n++
	return c.bus.Read(addr)
}

func (c *CPU) write(addr uint16, v uint8) {
	c.n++
	c.bus.Write(addr, v)
}

func (c *CPU) idle(k int) {
	for i := 0; i < k; i++ {
		c.n++
		c.bus.Idle()
	}
}

// dummy is the discarded opcode-slot read that implied/accumulator
// instructions perform.
func (c *CPU) dummy() { c.read(c.PC) }

// interrupt enters the highest-priority pending line: timer > IRQ1 > IRQ2.
func (c *CPU) interrupt(pending uint8) int {
	var vector uint16
	switch {
	case pending&IRQTimer != 0:
		vector = VecTimer
	case pending&IRQ1 != 0:
		vector = VecIRQ1
	default:
		vector = VecIRQ2
	}
	c.n = 0
	c.dummy()
	c.dummy()
	c.push16(c.PC)
	c.push(c.P &^ FlagB)
	c.P &^= FlagD | FlagT
	c.P |= FlagI
	c.PC = c.read16(vector)
	c.idle(1)
	c.Cycles += uint64(c.n)
	return c.n
}

// --- memory helpers ---

func (c *CPU) read16(addr uint16) uint16 {
	lo := uint16(c.read(addr))
	hi := uint16(c.read(addr + 1))
	return hi<<8 | lo
}

// readZpWord reads a 16-bit pointer from the zero page, wrapping $FF→$00
// inside the page rather than running into the stack (spec §2).
func (c *CPU) readZpWord(zp uint8) uint16 {
	lo := uint16(c.read(ZeroPage + uint16(zp)))
	hi := uint16(c.read(ZeroPage + uint16(uint8(zp+1))))
	return hi<<8 | lo
}

func (c *CPU) fetchByte() uint8 {
	v := c.read(c.PC)
	c.PC++
	return v
}

func (c *CPU) fetchWord() uint16 {
	lo := uint16(c.fetchByte())
	hi := uint16(c.fetchByte())
	return hi<<8 | lo
}

func (c *CPU) push(v uint8) {
	c.write(StackPage+uint16(c.S), v)
	c.S--
}

func (c *CPU) push16(v uint16) {
	c.push(uint8(v >> 8))
	c.push(uint8(v))
}

func (c *CPU) pop() uint8 {
	c.S++
	return c.read(StackPage + uint16(c.S))
}

func (c *CPU) pop16() uint16 {
	lo := uint16(c.pop())
	hi := uint16(c.pop())
	return hi<<8 | lo
}

// fetchOperand decodes the bytes after the opcode according to the mode and
// leaves the effective address / immediate in the CPU's operand fields.
func (c *CPU) fetchOperand() {
	switch c.op.Mode {
	case Imp, Acc:
		c.dummy()
	case Imm:
		c.imm = c.fetchByte()
	case Zp:
		c.addr = ZeroPage + uint16(c.fetchByte())
		c.idle(1)
	case ZpX:
		c.addr = ZeroPage + uint16(uint8(c.fetchByte()+c.X))
		c.idle(1)
	case ZpY:
		c.addr = ZeroPage + uint16(uint8(c.fetchByte()+c.Y))
		c.idle(1)
	case Abs:
		c.addr = c.fetchWord()
		c.idle(1)
	case AbsX:
		c.addr = c.fetchWord() + uint16(c.X)
		c.idle(1)
	case AbsY:
		c.addr = c.fetchWord() + uint16(c.Y)
		c.idle(1)
	case Ind:
		c.addr = c.fetchWord()
	case AbsXInd:
		c.addr = c.fetchWord() + uint16(c.X)
		c.idle(1)
	case IndX:
		zp := c.fetchByte()
		c.idle(1)
		c.addr = c.readZpWord(zp + c.X)
		c.idle(1)
	case IndY:
		zp := c.fetchByte()
		c.idle(1)
		c.addr = c.readZpWord(zp) + uint16(c.Y)
		c.idle(1)
	case ZpInd:
		zp := c.fetchByte()
		c.idle(1)
		c.addr = c.readZpWord(zp)
		c.idle(1)
	case Rel:
		c.rel = int8(c.fetchByte())
	case ZpRel:
		c.addr = ZeroPage + uint16(c.fetchByte())
		c.idle(1)
		c.rel = int8(c.fetchByte())
		c.idle(1)
	case ImmZp:
		c.imm = c.fetchByte()
		c.addr = ZeroPage + uint16(c.fetchByte())
		c.idle(1)
	case ImmZpX:
		c.imm = c.fetchByte()
		c.addr = ZeroPage + uint16(uint8(c.fetchByte()+c.X))
		c.idle(1)
	case ImmAbs:
		c.imm = c.fetchByte()
		c.addr = c.fetchWord()
		c.idle(1)
	case ImmAbsX:
		c.imm = c.fetchByte()
		c.addr = c.fetchWord() + uint16(c.X)
		c.idle(1)
	case Blk:
		c.dummy()
		c.idle(1)
		c.push(c.Y)
		c.push(c.A)
		c.push(c.X)
		c.blkSrc = c.fetchWord()
		c.blkDst = c.fetchWord()
		c.blkLen = c.fetchWord()
		c.idle(1)
	}
}

// operand returns the value an ALU instruction works on.
func (c *CPU) operand() uint8 {
	if c.op.Mode == Imm {
		return c.imm
	}
	return c.read(c.addr)
}

// --- flag helpers ---

func (c *CPU) setNZ(v uint8) uint8 {
	c.P &^= FlagN | FlagZ
	if v == 0 {
		c.P |= FlagZ
	} else if v&0x80 != 0 {
		c.P |= FlagN
	}
	return v
}

func (c *CPU) setFlag(flag uint8, on bool) {
	if on {
		c.P |= flag
	} else {
		c.P &^= flag
	}
}

// accIn/accOut implement the T flag (spec C1): with T set, the "accumulator"
// of ADC/SBC/AND/EOR/ORA is the zero-page byte at X, and A is untouched.
func (c *CPU) accIn() uint8 {
	if c.memFlag {
		v := c.read(ZeroPage + uint16(c.X))
		c.idle(1)
		return v
	}
	return c.A
}

func (c *CPU) accOut(v uint8) {
	c.setNZ(v)
	if c.memFlag {
		c.write(ZeroPage+uint16(c.X), v)
		return
	}
	c.A = v
}

func (c *CPU) adc(m uint8) {
	a := c.accIn()
	carry := uint16(c.P & FlagC)
	if c.P&FlagD != 0 {
		// Decimal mode (spec C2, strong inference): nibble-wise BCD add; V as
		// in the binary add; N/Z from the BCD result.
		lo := uint16(a&0x0F) + uint16(m&0x0F) + carry
		if lo > 9 {
			lo += 6
		}
		hi := uint16(a>>4) + uint16(m>>4)
		if lo > 0x0F {
			hi++
		}
		bin := uint16(a) + uint16(m) + carry
		c.setFlag(FlagV, (^(uint16(a)^uint16(m)))&(uint16(a)^bin)&0x80 != 0)
		if hi > 9 {
			hi += 6
		}
		c.setFlag(FlagC, hi > 0x0F)
		c.accOut(uint8(hi<<4) | uint8(lo&0x0F))
		return
	}
	sum := uint16(a) + uint16(m) + carry
	c.setFlag(FlagC, sum > 0xFF)
	c.setFlag(FlagV, (^(uint16(a)^uint16(m)))&(uint16(a)^sum)&0x80 != 0)
	c.accOut(uint8(sum))
}

func (c *CPU) sbc(m uint8) {
	a := c.accIn()
	borrow := uint16(1 - (c.P & FlagC))
	if c.P&FlagD != 0 {
		lo := int16(a&0x0F) - int16(m&0x0F) - int16(borrow)
		if lo < 0 {
			lo -= 6
		}
		hi := int16(a>>4) - int16(m>>4)
		if lo < 0 {
			hi--
		}
		diff := int16(a) - int16(m) - int16(borrow)
		c.setFlag(FlagV, (uint16(a)^uint16(m))&(uint16(a)^uint16(diff))&0x80 != 0)
		c.setFlag(FlagC, diff >= 0)
		if hi < 0 {
			hi -= 6
		}
		c.accOut(uint8(hi<<4) | uint8(lo&0x0F))
		return
	}
	diff := int16(a) - int16(m) - int16(borrow)
	c.setFlag(FlagC, diff >= 0)
	c.setFlag(FlagV, (uint16(a)^uint16(m))&(uint16(a)^uint16(diff))&0x80 != 0)
	c.accOut(uint8(diff))
}

func (c *CPU) compare(reg, m uint8) {
	diff := int16(reg) - int16(m)
	c.setFlag(FlagC, diff >= 0)
	c.setNZ(uint8(diff))
}

func (c *CPU) branch(taken bool) int {
	if !taken {
		return 0
	}
	c.dummy()
	c.idle(1)
	c.PC = uint16(int32(c.PC) + int32(c.rel))
	return 2
}

// bitBranch is BBR/BBS: a taken branch costs two idle cycles.
func (c *CPU) bitBranch(taken bool) int {
	if !taken {
		return 0
	}
	c.idle(2)
	c.PC = uint16(int32(c.PC) + int32(c.rel))
	return 2
}

// rmw applies f to the operand in place (memory or accumulator).
func (c *CPU) rmw(f func(uint8) uint8) {
	if c.op.Mode == Acc {
		c.A = f(c.A)
		return
	}
	v := c.read(c.addr)
	c.idle(1)
	c.write(c.addr, f(v))
}

func (c *CPU) blockTransfer(step func(i uint32, src, dst *uint16)) int {
	n := uint32(c.blkLen)
	if n == 0 {
		n = 0x10000
	}
	src, dst := c.blkSrc, c.blkDst
	for i := uint32(0); i < n; i++ {
		c.idle(1)
		v := c.read(src)
		c.idle(1)
		c.write(dst, v)
		c.idle(2)
		step(i, &src, &dst)
	}
	c.idle(1)
	c.X = c.pop()
	c.A = c.pop()
	c.Y = c.pop()
	return int(6 * n)
}

// execute runs the decoded instruction and returns the extra cycles beyond
// the table's base count.
func (c *CPU) execute() int {
	switch c.op.Name {
	// Loads and stores.
	case "LDA":
		c.A = c.setNZ(c.operand())
	case "LDX":
		c.X = c.setNZ(c.operand())
	case "LDY":
		c.Y = c.setNZ(c.operand())
	case "STA":
		c.write(c.addr, c.A)
	case "STX":
		c.write(c.addr, c.X)
	case "STY":
		c.write(c.addr, c.Y)
	case "STZ":
		c.write(c.addr, 0)

	// ALU.
	case "ADC":
		c.adc(c.operand())
	case "SBC":
		c.sbc(c.operand())
	case "AND":
		m := c.operand()
		c.accOut(c.accIn() & m)
	case "ORA":
		m := c.operand()
		c.accOut(c.accIn() | m)
	case "EOR":
		m := c.operand()
		c.accOut(c.accIn() ^ m)
	case "CMP":
		c.compare(c.A, c.operand())
	case "CPX":
		c.compare(c.X, c.operand())
	case "CPY":
		c.compare(c.Y, c.operand())
	case "BIT":
		v := c.operand()
		c.setFlag(FlagN, v&0x80 != 0)
		c.setFlag(FlagV, v&0x40 != 0)
		c.setFlag(FlagZ, c.A&v == 0)
	case "TST":
		c.idle(1)
		v := c.read(c.addr)
		c.idle(1)
		c.setFlag(FlagN, v&0x80 != 0)
		c.setFlag(FlagV, v&0x40 != 0)
		c.setFlag(FlagZ, c.imm&v == 0)
	case "TSB":
		v := c.read(c.addr)
		c.idle(1)
		c.setFlag(FlagN, v&0x80 != 0)
		c.setFlag(FlagV, v&0x40 != 0)
		c.setFlag(FlagZ, c.A&v == 0)
		c.write(c.addr, v|c.A)
	case "TRB":
		v := c.read(c.addr)
		c.idle(1)
		c.setFlag(FlagN, v&0x80 != 0)
		c.setFlag(FlagV, v&0x40 != 0)
		c.setFlag(FlagZ, c.A&v == 0)
		c.write(c.addr, v&^c.A)

	// Shifts and increments.
	case "ASL":
		c.rmw(func(v uint8) uint8 { c.setFlag(FlagC, v&0x80 != 0); return c.setNZ(v << 1) })
	case "LSR":
		c.rmw(func(v uint8) uint8 { c.setFlag(FlagC, v&0x01 != 0); return c.setNZ(v >> 1) })
	case "ROL":
		c.rmw(func(v uint8) uint8 {
			in := c.P & FlagC
			c.setFlag(FlagC, v&0x80 != 0)
			return c.setNZ(v<<1 | in)
		})
	case "ROR":
		c.rmw(func(v uint8) uint8 {
			in := (c.P & FlagC) << 7
			c.setFlag(FlagC, v&0x01 != 0)
			return c.setNZ(v>>1 | in)
		})
	case "INC":
		c.rmw(func(v uint8) uint8 { return c.setNZ(v + 1) })
	case "DEC":
		c.rmw(func(v uint8) uint8 { return c.setNZ(v - 1) })
	case "INX":
		c.X = c.setNZ(c.X + 1)
	case "INY":
		c.Y = c.setNZ(c.Y + 1)
	case "DEX":
		c.X = c.setNZ(c.X - 1)
	case "DEY":
		c.Y = c.setNZ(c.Y - 1)

	// Register transfers and swaps.
	case "TAX":
		c.X = c.setNZ(c.A)
	case "TAY":
		c.Y = c.setNZ(c.A)
	case "TXA":
		c.A = c.setNZ(c.X)
	case "TYA":
		c.A = c.setNZ(c.Y)
	case "TSX":
		c.X = c.setNZ(c.S)
	case "TXS":
		c.S = c.X
	case "SXY":
		c.idle(1)
		c.X, c.Y = c.Y, c.X
	case "SAX":
		c.idle(1)
		c.A, c.X = c.X, c.A
	case "SAY":
		c.idle(1)
		c.A, c.Y = c.Y, c.A
	case "CLA":
		c.A = 0
	case "CLX":
		c.X = 0
	case "CLY":
		c.Y = 0

	// Stack.
	case "PHA":
		c.push(c.A)
	case "PHX":
		c.push(c.X)
	case "PHY":
		c.push(c.Y)
	case "PHP":
		c.push(c.P | FlagB)
	case "PLA":
		c.idle(1)
		c.A = c.setNZ(c.pop())
	case "PLX":
		c.idle(1)
		c.X = c.setNZ(c.pop())
	case "PLY":
		c.idle(1)
		c.Y = c.setNZ(c.pop())
	case "PLP":
		c.idle(1)
		c.P = c.pop() &^ FlagB // P never holds B; it only appears on the pushed copy

	// Flags.
	case "CLC":
		c.P &^= FlagC
	case "SEC":
		c.P |= FlagC
	case "CLI":
		c.P &^= FlagI
	case "SEI":
		c.P |= FlagI
	case "CLD":
		c.P &^= FlagD
	case "SED":
		c.P |= FlagD
	case "CLV":
		c.P &^= FlagV
	case "SET":
		c.P |= FlagT

	// Control flow.
	case "JMP":
		switch c.op.Mode {
		case Ind:
			c.idle(1)
			c.PC = c.read16(c.addr)
			c.idle(1)
		case AbsXInd:
			c.PC = c.read16(c.addr)
			c.idle(1)
		default:
			c.PC = c.addr
		}
	case "JSR":
		// The absolute operand was fetched as two bytes plus an idle; the
		// hardware interleaves the push between the two bytes, which costs
		// the same seven cycles and ends in the same state.
		c.idle(1)
		c.push16(c.PC - 1)
		c.PC = c.addr
	case "BSR":
		c.idle(1)
		c.push16(c.PC - 1)
		c.idle(3)
		c.PC = uint16(int32(c.PC) + int32(c.rel))
	case "RTS":
		c.idle(1)
		c.PC = c.pop16() + 1
		c.idle(2)
	case "RTI":
		c.idle(1)
		c.P = c.pop() &^ FlagB
		c.PC = c.pop16()
		c.idle(1)
	case "BRK":
		// The byte after BRK was consumed as an immediate (spec §4); the
		// pushed address is the one after it, as on the 6502.
		c.push16(c.PC)
		c.push(c.P | FlagB)
		c.P &^= FlagD | FlagT
		c.P |= FlagI
		c.PC = c.read16(VecIRQ2)
		c.idle(1)
	case "BRA":
		c.idle(2)
		c.PC = uint16(int32(c.PC) + int32(c.rel))
	case "BPL":
		return c.branch(c.P&FlagN == 0)
	case "BMI":
		return c.branch(c.P&FlagN != 0)
	case "BVC":
		return c.branch(c.P&FlagV == 0)
	case "BVS":
		return c.branch(c.P&FlagV != 0)
	case "BCC":
		return c.branch(c.P&FlagC == 0)
	case "BCS":
		return c.branch(c.P&FlagC != 0)
	case "BNE":
		return c.branch(c.P&FlagZ == 0)
	case "BEQ":
		return c.branch(c.P&FlagZ != 0)

	// Zero-page bit instructions.
	case "RMB0", "RMB1", "RMB2", "RMB3", "RMB4", "RMB5", "RMB6", "RMB7":
		bit := c.op.Name[3] - '0'
		v := c.read(c.addr)
		c.idle(2)
		c.write(c.addr, v&^(1<<bit))
	case "SMB0", "SMB1", "SMB2", "SMB3", "SMB4", "SMB5", "SMB6", "SMB7":
		bit := c.op.Name[3] - '0'
		v := c.read(c.addr)
		c.idle(2)
		c.write(c.addr, v|(1<<bit))
	case "BBR0", "BBR1", "BBR2", "BBR3", "BBR4", "BBR5", "BBR6", "BBR7":
		bit := c.op.Name[3] - '0'
		return c.bitBranch(c.read(c.addr)&(1<<bit) == 0)
	case "BBS0", "BBS1", "BBS2", "BBS3", "BBS4", "BBS5", "BBS6", "BBS7":
		bit := c.op.Name[3] - '0'
		return c.bitBranch(c.read(c.addr)&(1<<bit) != 0)

	// Hudson extensions.
	case "ST0":
		c.idle(1)
		c.n++
		c.sampleIRQ()
		c.bus.WriteVDCPort(0, c.imm)
	case "ST1":
		c.idle(1)
		c.n++
		c.sampleIRQ()
		c.bus.WriteVDCPort(2, c.imm)
	case "ST2":
		c.idle(1)
		c.n++
		c.sampleIRQ()
		c.bus.WriteVDCPort(3, c.imm)
	case "TAM":
		c.idle(3)
		c.bus.SetMPR(c.imm, c.A)
	case "TMA":
		c.idle(2)
		c.A = c.bus.GetMPR(c.imm)
	case "CSL":
		c.bus.SetSpeed(false)
		c.idle(1)
	case "CSH":
		c.bus.SetSpeed(true)
		c.idle(1)
	case "TII":
		return c.blockTransfer(func(_ uint32, src, dst *uint16) { *src++; *dst++ })
	case "TDD":
		return c.blockTransfer(func(_ uint32, src, dst *uint16) { *src--; *dst-- })
	case "TIN":
		return c.blockTransfer(func(_ uint32, src, _ *uint16) { *src++ })
	case "TIA":
		return c.blockTransfer(func(i uint32, src, dst *uint16) {
			*src++
			if i&1 == 0 {
				*dst++
			} else {
				*dst--
			}
		})
	case "TAI":
		return c.blockTransfer(func(i uint32, src, dst *uint16) {
			if i&1 == 0 {
				*src++
			} else {
				*src--
			}
			*dst++
		})

	case "NOP":
	}
	return 0
}

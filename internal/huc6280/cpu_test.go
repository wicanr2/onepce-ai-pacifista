package huc6280

import "testing"

// testBus is a flat 64 KiB logical space with a fake I/O side: enough to
// exercise every instruction without a mapper.
type testBus struct {
	mem     [0x10000]uint8
	mpr     [8]uint8
	mprLast uint8
	vdc     []struct{ port, value uint8 }
	fast    bool
	ticks   int
	pending uint8
}

func (b *testBus) Read(a uint16) uint8     { b.ticks++; return b.mem[a] }
func (b *testBus) Write(a uint16, v uint8) { b.ticks++; b.mem[a] = v }
func (b *testBus) Peek(a uint16) uint8     { return b.mem[a] }
func (b *testBus) Idle()                   { b.ticks++ }
func (b *testBus) WriteVDCPort(port, value uint8) {
	b.ticks++
	b.vdc = append(b.vdc, struct{ port, value uint8 }{port, value})
}
func (b *testBus) SetMPR(mask, value uint8) {
	if mask == 0 {
		return
	}
	b.mprLast = value
	for i := 0; i < 8; i++ {
		if mask&(1<<i) != 0 {
			b.mpr[i] = value
		}
	}
}
func (b *testBus) GetMPR(mask uint8) uint8 {
	if mask == 0 {
		return b.mprLast
	}
	var v uint8
	for i := 0; i < 8; i++ {
		if mask&(1<<i) != 0 {
			v |= b.mpr[i]
		}
	}
	b.mprLast = v
	return v
}
func (b *testBus) SetSpeed(fast bool)            { b.fast = fast }
func (b *testBus) PendingIRQ() uint8             { return b.pending }
func (b *testBus) load(at uint16, code ...uint8) { copy(b.mem[at:], code) }

func newCPU(t *testing.T, code ...uint8) (*CPU, *testBus) {
	t.Helper()
	b := &testBus{}
	b.mem[VecReset] = 0x00
	b.mem[VecReset+1] = 0x80 // reset to $8000
	b.load(0x8000, code...)
	c := New(b)
	c.Reset()
	c.S = 0xFF // hardware leaves S undefined; tests want a stack that does not wrap
	return c, b
}

func TestResetTakesTheVectorAndSetsI(t *testing.T) {
	c, _ := newCPU(t)
	if c.PC != 0x8000 || c.P != FlagI {
		t.Fatalf("PC=$%04X P=$%02X after reset", c.PC, c.P)
	}
}

func TestLoadStoreAndZeroPageBase(t *testing.T) {
	// LDA #$42 ; STA $10 ; LDX $10 → zero page lives at $2000.
	c, b := newCPU(t, 0xA9, 0x42, 0x85, 0x10, 0xA6, 0x10)
	c.Step()
	c.Step()
	c.Step()
	if b.mem[0x2010] != 0x42 || c.X != 0x42 {
		t.Fatalf("zp write landed wrong: mem[$2010]=%02X X=%02X", b.mem[0x2010], c.X)
	}
	if b.ticks != 2+4+4 {
		t.Fatalf("cycles %d, want 10", b.ticks)
	}
}

func TestIndirectZeroPageWrapsInsideThePage(t *testing.T) {
	// LDA ($FF) with pointer bytes at $20FF (lo) and $2000 (hi), not $2100.
	c, b := newCPU(t, 0xB2, 0xFF)
	b.mem[0x20FF] = 0x34
	b.mem[0x2000] = 0x12
	b.mem[0x2100] = 0xEE // the stack byte must not be used
	b.mem[0x1234] = 0x77
	c.Step()
	if c.A != 0x77 {
		t.Fatalf("A=%02X, want 77 (pointer must wrap to $2000)", c.A)
	}
}

func TestTFlagRedirectsTheAccumulatorToZeroPageX(t *testing.T) {
	// LDX #$05 ; LDA #$F0 ; SET ; ORA #$0F ; (next) NOP
	c, b := newCPU(t, 0xA2, 0x05, 0xA9, 0xF0, 0xF4, 0x09, 0x0F, 0xEA)
	b.mem[0x2005] = 0x30
	for i := 0; i < 4; i++ {
		c.Step()
	}
	if b.mem[0x2005] != 0x3F {
		t.Fatalf("zp[X] = %02X, want 3F (memory operand mode)", b.mem[0x2005])
	}
	if c.A != 0xF0 {
		t.Fatalf("A changed to %02X; T-mode ORA must leave A alone", c.A)
	}
	if c.P&FlagT != 0 {
		t.Fatal("T must be cleared after the instruction it applied to")
	}
	c.Step() // NOP: T no longer applies
	if c.P&FlagT != 0 {
		t.Fatal("T leaked into a later instruction")
	}
}

func TestBlockTransferDirections(t *testing.T) {
	cases := []struct {
		name   string
		opcode uint8
		want   [4]uint8 // dst $3000..$3003 after copying 4 bytes from $2800..
	}{
		{"TII", 0x73, [4]uint8{1, 2, 3, 4}},
		{"TIN", 0xD3, [4]uint8{4, 0, 0, 0}}, // dst fixed → last byte wins
		{"TIA", 0xE3, [4]uint8{3, 4, 0, 0}}, // dst alternates 0,1,0,1 → [3,4]
		{"TAI", 0xF3, [4]uint8{1, 2, 1, 2}}, // src alternates 0,1,0,1
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, b := newCPU(t, tc.opcode, 0x00, 0x28, 0x00, 0x30, 0x04, 0x00)
			b.load(0x2800, 1, 2, 3, 4)
			c.A, c.X, c.Y = 0xAA, 0xBB, 0xCC
			cycles := c.Step()
			if got := [4]uint8{b.mem[0x3000], b.mem[0x3001], b.mem[0x3002], b.mem[0x3003]}; got != tc.want {
				t.Fatalf("dst = %v, want %v", got, tc.want)
			}
			if c.A != 0xAA || c.X != 0xBB || c.Y != 0xCC {
				t.Fatalf("registers must survive: A=%02X X=%02X Y=%02X", c.A, c.X, c.Y)
			}
			if cycles != 17+6*4 {
				t.Fatalf("cycles %d, want %d", cycles, 17+6*4)
			}
			if c.PC != 0x8007 {
				t.Fatalf("PC=$%04X, want $8007 (7-byte instruction)", c.PC)
			}
		})
	}
	t.Run("TDD", func(t *testing.T) {
		// Copy $2803..$2800 down to $3003..$3000.
		c, b := newCPU(t, 0xC3, 0x03, 0x28, 0x03, 0x30, 0x04, 0x00)
		b.load(0x2800, 1, 2, 3, 4)
		c.Step()
		if got := [4]uint8{b.mem[0x3000], b.mem[0x3001], b.mem[0x3002], b.mem[0x3003]}; got != [4]uint8{1, 2, 3, 4} {
			t.Fatalf("dst = %v", got)
		}
	})
}

func TestTAMWritesEverySelectedRegisterAndTMAOrsThem(t *testing.T) {
	// LDA #$12 ; TAM #$05 (MPR0,MPR2) ; LDA #$40 ; TAM #$08 ; TMA #$0C (MPR2|MPR3) ; TMA #$00
	c, b := newCPU(t, 0xA9, 0x12, 0x53, 0x05, 0xA9, 0x40, 0x53, 0x08, 0x43, 0x0C, 0x43, 0x00)
	for i := 0; i < 5; i++ {
		c.Step()
	}
	if b.mpr[0] != 0x12 || b.mpr[2] != 0x12 || b.mpr[3] != 0x40 || b.mpr[1] != 0 {
		t.Fatalf("mpr=%v", b.mpr)
	}
	if c.A != 0x52 {
		t.Fatalf("TMA #$0C gave %02X, want 12|40=52", c.A)
	}
	c.A = 0
	c.Step()
	if c.A != 0x52 {
		t.Fatalf("TMA #0 gave %02X, want the last TMA/TAM value 52", c.A)
	}
}

func TestST0ST1ST2WriteVDCPortsWithoutPaging(t *testing.T) {
	c, b := newCPU(t, 0x03, 0x05, 0x13, 0x10, 0x23, 0x20)
	c.Step()
	c.Step()
	c.Step()
	want := []struct{ port, value uint8 }{{0, 0x05}, {2, 0x10}, {3, 0x20}}
	if len(b.vdc) != 3 {
		t.Fatalf("vdc writes = %v", b.vdc)
	}
	for i, w := range want {
		if b.vdc[i] != w {
			t.Fatalf("write %d = %v, want %v", i, b.vdc[i], w)
		}
	}
}

func TestInterruptPriorityAndMasking(t *testing.T) {
	c, b := newCPU(t, 0x58, 0xEA, 0xEA) // CLI ; NOP ; NOP
	b.mem[VecTimer], b.mem[VecTimer+1] = 0x00, 0x90
	b.mem[VecIRQ1], b.mem[VecIRQ1+1] = 0x00, 0xA0
	b.mem[VecIRQ2], b.mem[VecIRQ2+1] = 0x00, 0xB0
	c.P |= FlagD | FlagT
	b.pending = IRQ2 | IRQ1 | IRQTimer
	c.Step() // CLI: its last cycle still saw I set, so nothing is taken yet
	if c.PC != 0x8001 {
		t.Fatalf("PC=$%04X: the interrupt must wait one instruction after CLI", c.PC)
	}
	c.Step() // NOP: taken after this one
	if c.PC != 0x9000 {
		t.Fatalf("PC=$%04X, want the timer vector $9000 (timer outranks IRQ1/IRQ2)", c.PC)
	}
	if c.P&FlagI == 0 || c.P&(FlagD|FlagT) != 0 {
		t.Fatalf("P=%02X: entering an interrupt must set I and clear D and T", c.P)
	}
	// The pushed P has B clear and still carries the old D/T.
	pushed := b.mem[StackPage+uint16(c.S)+1]
	if pushed&FlagB != 0 || pushed&FlagD == 0 {
		t.Fatalf("pushed P=%02X", pushed)
	}
	// Return address is the second NOP.
	ret := uint16(b.mem[StackPage+uint16(c.S)+2]) | uint16(b.mem[StackPage+uint16(c.S)+3])<<8
	if ret != 0x8002 {
		t.Fatalf("pushed PC=$%04X, want $8002", ret)
	}

	// With I set nothing is taken.
	c2, b2 := newCPU(t, 0xEA)
	b2.pending = IRQ2
	c2.Step()
	if c2.PC != 0x8001 {
		t.Fatalf("interrupt taken while I set: PC=$%04X", c2.PC)
	}
}

func TestBRKUsesTheIRQ2VectorAndPushesB(t *testing.T) {
	c, b := newCPU(t, 0x00, 0xFF, 0xEA)
	b.mem[VecIRQ2], b.mem[VecIRQ2+1] = 0x00, 0xB0
	c.Step()
	if c.PC != 0xB000 {
		t.Fatalf("PC=$%04X", c.PC)
	}
	if b.mem[StackPage+uint16(c.S)+1]&FlagB == 0 {
		t.Fatal("BRK must push P with B set")
	}
	ret := uint16(b.mem[StackPage+uint16(c.S)+2]) | uint16(b.mem[StackPage+uint16(c.S)+3])<<8
	if ret != 0x8002 {
		t.Fatalf("pushed PC=$%04X, want $8002 (byte after the BRK padding)", ret)
	}
}

func TestBranchesBitBranchesAndBSR(t *testing.T) {
	// BBS0 $10,+2 ; NOP ; NOP ; BSR -6 (back to $8000)
	c, b := newCPU(t, 0x8F, 0x10, 0x02, 0xEA, 0xEA, 0x44, 0xF9)
	b.mem[0x2010] = 0x01
	if cycles := c.Step(); cycles != 8 || c.PC != 0x8005 {
		t.Fatalf("BBS taken: cycles=%d PC=$%04X", cycles, c.PC)
	}
	if cycles := c.Step(); cycles != 8 || c.PC != 0x8000 {
		t.Fatalf("BSR: cycles=%d PC=$%04X", cycles, c.PC)
	}
	// BSR pushed the address of its last byte, so RTS lands after it.
	ret := uint16(b.mem[StackPage+uint16(c.S)+1]) | uint16(b.mem[StackPage+uint16(c.S)+2])<<8
	if ret != 0x8006 {
		t.Fatalf("BSR pushed $%04X, want $8006", ret)
	}
}

func TestDecimalADCAndSBC(t *testing.T) {
	// SED ; LDA #$19 ; ADC #$01 → $20 (C clear) ; SEC ; SBC #$01 → $19
	c, _ := newCPU(t, 0xF8, 0xA9, 0x19, 0x18, 0x69, 0x01, 0x38, 0xE9, 0x01)
	for i := 0; i < 4; i++ {
		c.Step()
	}
	if c.A != 0x20 || c.P&FlagC != 0 {
		t.Fatalf("BCD add: A=%02X P=%02X", c.A, c.P)
	}
	c.Step()
	c.Step()
	if c.A != 0x19 || c.P&FlagC == 0 {
		t.Fatalf("BCD sub: A=%02X P=%02X", c.A, c.P)
	}
}

func TestCompareAndShiftFlags(t *testing.T) {
	// LDA #$80 ; CMP #$01 (C=1,N=0) ; ASL A (C=1, A=0, Z=1)
	c, _ := newCPU(t, 0xA9, 0x80, 0xC9, 0x01, 0x0A)
	c.Step()
	c.Step()
	if c.P&FlagC == 0 || c.P&FlagN != 0 || c.P&FlagZ != 0 {
		t.Fatalf("CMP flags P=%02X", c.P)
	}
	c.Step()
	if c.A != 0 || c.P&FlagC == 0 || c.P&FlagZ == 0 {
		t.Fatalf("ASL: A=%02X P=%02X", c.A, c.P)
	}
}

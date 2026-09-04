package vce

import "testing"

// H-031: palette address then data low/high, address advancing after the
// high byte; reads advance the same way.
func TestPaletteWriteAndReadSequence(t *testing.T) {
	c := New()
	c.Write(2, 0x10)
	c.Write(3, 0x01) // address $110
	c.Write(4, 0xAB)
	c.Write(5, 0x01) // $1AB, then advance
	c.Write(4, 0x34)
	c.Write(5, 0x00) // $034 at $111
	if c.Color(0x110) != 0x1AB || c.Color(0x111) != 0x034 {
		t.Fatalf("palette %03X %03X", c.Color(0x110), c.Color(0x111))
	}
	c.Write(2, 0x10)
	c.Write(3, 0x01)
	if lo, hi := c.Read(4), c.Read(5); lo != 0xAB || hi != 0xFF {
		t.Fatalf("read %02X %02X (high byte carries bit8 with bits 1–7 set)", lo, hi)
	}
	if c.Read(4) != 0x34 {
		t.Fatal("read address must advance after the high byte")
	}
}

func TestControlRegisterSelectsDividerAndLines(t *testing.T) {
	c := New()
	if c.ClockDivider() != 4 || c.Lines() != 262 {
		t.Fatalf("power-on divider %d lines %d", c.ClockDivider(), c.Lines())
	}
	c.Write(0, 0x05) // 7 MHz, 263 lines
	if c.ClockDivider() != 3 || c.Lines() != 263 {
		t.Fatalf("divider %d lines %d", c.ClockDivider(), c.Lines())
	}
}

func TestRGBExpandsGRB(t *testing.T) {
	r, g, b := RGB(0x1C0) // G=7, R=0, B=0
	if g != 252 || r != 0 || b != 0 {
		t.Fatalf("rgb=%d,%d,%d", r, g, b)
	}
}

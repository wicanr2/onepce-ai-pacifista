// Package vce is the HuC6260 video colour encoder: the 512-entry 9-bit
// palette and the dot clock. Spec: docs/spec/vdc-vce.md §1, §7.
// 參考行為：Mesen2 PceVce.cpp @ b9fa69d §register semantics（只取行為事實）。
package vce

// VCE holds the palette and the timing control register.
type VCE struct {
	palette  [0x200]uint16
	addr     uint16
	divider  int
	lines    int
	grayscal bool
}

// New returns a VCE in its power-on state: divider 4, 262 lines.
func New() *VCE {
	return &VCE{divider: 4, lines: 262}
}

// Read implements bus.Device for offsets $0400–$0407 (low 3 bits).
func (c *VCE) Read(off uint16) uint8 {
	switch off & 0x07 {
	case 4:
		return uint8(c.palette[c.addr])
	case 5:
		v := uint8(c.palette[c.addr]>>8) & 0x01
		c.addr = (c.addr + 1) & 0x1FF
		return 0xFE | v
	}
	return 0xFF
}

// Write implements bus.Device.
func (c *VCE) Write(off uint16, value uint8) {
	switch off & 0x07 {
	case 0:
		if value&0x04 != 0 {
			c.lines = 263
		} else {
			c.lines = 262
		}
		c.grayscal = value&0x80 != 0
		switch value & 0x03 {
		case 0:
			c.divider = 4
		case 1:
			c.divider = 3
		default:
			c.divider = 2
		}
	case 2:
		c.addr = c.addr&0x100 | uint16(value)
	case 3:
		c.addr = c.addr&0xFF | uint16(value&0x01)<<8
	case 4:
		c.palette[c.addr] = c.palette[c.addr]&0x100 | uint16(value)
	case 5:
		c.palette[c.addr] = c.palette[c.addr]&0xFF | uint16(value&0x01)<<8
		c.addr = (c.addr + 1) & 0x1FF
	}
}

// ClockDivider is master clocks per dot (4, 3 or 2).
func (c *VCE) ClockDivider() int { return c.divider }

// Lines is scanlines per frame (262 or 263).
func (c *VCE) Lines() int { return c.lines }

// Grayscale reports the control register's bit 7.
func (c *VCE) Grayscale() bool { return c.grayscal }

// Color returns palette entry i as 9-bit GRB (bits 8–6 G, 5–3 R, 2–0 B).
func (c *VCE) Color(i uint16) uint16 { return c.palette[i&0x1FF] }

// Palette returns a copy of all 512 entries for snapshots.
func (c *VCE) Palette() []uint16 {
	out := make([]uint16, len(c.palette))
	copy(out, c.palette[:])
	return out
}

// RGB expands a 9-bit GRB entry to 8-bit channels (3-bit channel × 36 ≈ 255).
func RGB(color uint16) (r, g, b uint8) {
	g = uint8((color>>6)&0x07) * 36
	r = uint8((color>>3)&0x07) * 36
	b = uint8(color&0x07) * 36
	return
}

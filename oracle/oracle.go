// Package oracle holds pure decoders and comparators over snapshot words
// (VRAM, SAT, VCE palette, VDC registers) so that go tests and the GUI do
// not each re-implement the tile, sprite and BAT layouts. Spec:
// docs/spec/oracle-helpers.md. Nothing here runs the machine.
//
// 參考行為：docs/spec/vdc-vce.md §6（H-030 圖塊、SAT、BAT 版面）；
// Mesen2 PceConstants.h／PceDefaultVideoFilter.h @ b9fa69d（畫面傾印座標，只取事實）。
package oracle

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// Sprite is one decoded SAT entry in screen coordinates.
type Sprite struct {
	Index, X, Y, Width, Height int
	Cell                       uint16 // pattern cell number with the size masks applied
	Palette                    uint8
	HFlip, VFlip, Front, SP23  bool
}

// Sprites decodes all 64 SAT entries in table order. SAT shorter than 256
// words yields fewer sprites.
func Sprites(sat []uint16) []Sprite {
	out := make([]Sprite, 0, 64)
	for i := 0; i*4+3 < len(sat) && i < 64; i++ {
		attr := sat[i*4+3]
		s := Sprite{
			Index:   i,
			Y:       int(sat[i*4]&0x3FF) - 64,
			X:       int(sat[i*4+1]&0x3FF) - 32,
			Width:   16,
			Height:  16,
			Palette: uint8(attr & 0x0F),
			HFlip:   attr&0x0800 != 0,
			VFlip:   attr&0x8000 != 0,
			Front:   attr&0x0080 != 0,
			SP23:    sat[i*4+2]&0x01 != 0,
		}
		cell := (sat[i*4+2] & 0x7FF) >> 1
		if attr&0x0100 != 0 {
			s.Width = 32
			cell &^= 0x01
		}
		switch (attr >> 12) & 0x03 {
		case 1:
			s.Height = 32
			cell &^= 0x02
		case 2, 3:
			s.Height = 64
			cell &^= 0x06
		}
		s.Cell = cell
		out = append(out, s)
	}
	return out
}

// Visible reports whether the sprite overlaps a w×h display window.
func (s Sprite) Visible(w, h int) bool {
	return s.X < w && s.X+s.Width > 0 && s.Y < h && s.Y+s.Height > 0
}

// TilePixel decodes pixel (x,y) of a 4bpp background tile (16 words per
// tile, H-030 layout). Out-of-range inputs return 0.
func TilePixel(vram []uint16, tile, x, y int) uint8 {
	base := tile * 16
	if x < 0 || x > 7 || y < 0 || y > 7 || base < 0 || base+15 >= len(vram) {
		return 0
	}
	p01, p23 := vram[base+y], vram[base+y+8]
	shift := uint(7 - x)
	return uint8((p01>>shift)&1) | uint8((p01>>(8+shift))&1)<<1 |
		uint8((p23>>shift)&1)<<2 | uint8((p23>>(8+shift))&1)<<3
}

// SpritePixel decodes pixel (x,y) inside sprite s (4bpp, 64 words per 16×16
// cell: planes at +0/+16/+32/+48, bit 15 leftmost), flips applied.
func SpritePixel(vram []uint16, s Sprite, x, y int) uint8 {
	if x < 0 || x >= s.Width || y < 0 || y >= s.Height {
		return 0
	}
	if s.HFlip {
		x = s.Width - 1 - x
	}
	if s.VFlip {
		y = s.Height - 1 - y
	}
	cell := s.Cell | uint16(y>>4)<<1 | uint16(x>>4)
	a := int(cell)*64 + (y & 0x0F)
	if a+48 >= len(vram) {
		return 0
	}
	shift := uint(15 - (x & 0x0F))
	return uint8((vram[a]>>shift)&1) | uint8((vram[a+16]>>shift)&1)<<1 |
		uint8((vram[a+32]>>shift)&1)<<2 | uint8((vram[a+48]>>shift)&1)<<3
}

// BATEntry is one background attribute word.
type BATEntry struct {
	Tile    uint16
	Palette uint8
}

// BATSize returns the BAT dimensions selected by MWR bits 4–6.
func BATSize(mwr uint16) (cols, rows int) {
	switch (mwr >> 4) & 0x03 {
	case 0:
		cols = 32
	case 1:
		cols = 64
	default:
		cols = 128
	}
	rows = 32
	if mwr&0x40 != 0 {
		rows = 64
	}
	return cols, rows
}

// BAT decodes the background attribute table at VRAM word 0.
func BAT(vram []uint16, mwr uint16) (cols, rows int, entries []BATEntry) {
	cols, rows = BATSize(mwr)
	n := cols * rows
	if n > len(vram) {
		n = len(vram)
	}
	entries = make([]BATEntry, n)
	for i := 0; i < n; i++ {
		entries[i] = BATEntry{Tile: vram[i] & 0x0FFF, Palette: uint8(vram[i] >> 12)}
	}
	return cols, rows, entries
}

// RGB expands a 9-bit VCE colour (GRB, 3 bits each) linearly to 8 bits.
func RGB(c uint16) (r, g, b uint8) {
	lvl := func(v uint16) uint8 { return uint8((v&7)*255/7 + 0) }
	return lvl(c >> 3), lvl(c >> 6), lvl(c)
}

// PixelChange is one tile pixel whose value differs between two VRAM images.
type PixelChange struct {
	Tile, X, Y    int
	Before, After uint8
}

// ChangedTilePixels lists every tile pixel in the word range [loWord, hiWord)
// that differs between before and after, in tile/row/column order.
func ChangedTilePixels(before, after []uint16, loWord, hiWord int) []PixelChange {
	if hiWord > len(before) {
		hiWord = len(before)
	}
	if hiWord > len(after) {
		hiWord = len(after)
	}
	var out []PixelChange
	for tile := loWord / 16; tile*16+15 < hiWord; tile++ {
		base := tile * 16
		for row := 0; row < 8; row++ {
			if before[base+row] == after[base+row] && before[base+row+8] == after[base+row+8] {
				continue
			}
			for x := 0; x < 8; x++ {
				b, a := TilePixel(before, tile, x, row), TilePixel(after, tile, x, row)
				if b != a {
					out = append(out, PixelChange{Tile: tile, X: x, Y: row, Before: b, After: a})
				}
			}
		}
	}
	return out
}

// Screen is an RGB picture dumped by the oracle (0xRRGGBB per pixel).
type Screen struct {
	W, H int
	RGB  []uint32
}

// ReadMesen2Screen parses a screen-<frame>.bin written by
// tools/oracle/mesen2_state_probe.lua.
func ReadMesen2Screen(r io.Reader) (*Screen, error) {
	br := bufio.NewReader(r)
	head, err := br.ReadString('\n')
	if err != nil {
		return nil, fmt.Errorf("screen header: %w", err)
	}
	f := strings.Split(strings.TrimRight(head, "\n"), "\t")
	if len(f) != 3 || f[0] != "onepce-mesen2-screen-v1" {
		return nil, fmt.Errorf("screen header %q: want onepce-mesen2-screen-v1\\tw\\th", head)
	}
	w, err1 := strconv.Atoi(f[1])
	h, err2 := strconv.Atoi(f[2])
	if err1 != nil || err2 != nil || w <= 0 || h <= 0 {
		return nil, fmt.Errorf("screen header %q: bad size", head)
	}
	buf := make([]byte, w*h*3)
	if _, err := io.ReadFull(br, buf); err != nil {
		return nil, fmt.Errorf("screen pixels: %w", err)
	}
	s := &Screen{W: w, H: h, RGB: make([]uint32, w*h)}
	for i := range s.RGB {
		s.RGB[i] = uint32(buf[3*i])<<16 | uint32(buf[3*i+1])<<8 | uint32(buf[3*i+2])
	}
	return s, nil
}

// Mesen2LeftOverscan is the first dot of a row that Mesen2's default filter
// shows, per VCE clock divider (PceConstants::GetLeftOverscan).
func Mesen2LeftOverscan(clockDivider int) int {
	switch clockDivider {
	case 2:
		return 240/2 - 18*2
	case 3:
		return 216/3 - 18*4/3
	default:
		return 192/4 - 18
	}
}

// ScreenMatch is the result of comparing a native picture with a reference.
type ScreenMatch struct {
	X0, Y0   int // where native (0,0) sits in the reference
	Compared int // pixels present in both
	Mismatch int // pixels that differ (colour-map conflicts included)
	Colours  int // size of the learned 9-bit → RGB map
}

// MatchScreen compares a w×h native 9-bit picture with ref, placing native
// (0,0) at (x0,y0) in ref. Colours are compared through a one-to-one map
// learned from the picture itself (docs/spec/framebuffer-parity.md §4).
func MatchScreen(w, h int, native []uint16, ref *Screen, x0, y0 int) ScreenMatch {
	m := ScreenMatch{X0: x0, Y0: y0}
	if ref == nil || len(native) < w*h {
		return m
	}
	toRGB := map[uint16]uint32{}
	fromRGB := map[uint32]uint16{}
	for y := 0; y < h; y++ {
		ry := y + y0
		if ry < 0 || ry >= ref.H {
			continue
		}
		for x := 0; x < w; x++ {
			rx := x + x0
			if rx < 0 || rx >= ref.W {
				continue
			}
			m.Compared++
			c := native[y*w+x] & 0x1FF
			rgb := ref.RGB[ry*ref.W+rx]
			want, seen := toRGB[c]
			if !seen {
				if owner, taken := fromRGB[rgb]; taken && owner != c {
					m.Mismatch++
					continue
				}
				toRGB[c], fromRGB[rgb] = rgb, c
				continue
			}
			if want != rgb {
				m.Mismatch++
			}
		}
	}
	m.Colours = len(toRGB)
	return m
}

// SearchScreen tries every placement within ±radius of (x0,y0) and returns
// the one with the fewest mismatches (ties keep the placement nearest the
// guess, then the earliest scanned).
func SearchScreen(w, h int, native []uint16, ref *Screen, x0, y0, radius int) ScreenMatch {
	best := MatchScreen(w, h, native, ref, x0, y0)
	for dy := -radius; dy <= radius; dy++ {
		for dx := -radius; dx <= radius; dx++ {
			if dx == 0 && dy == 0 {
				continue
			}
			c := MatchScreen(w, h, native, ref, x0+dx, y0+dy)
			if c.Compared > 0 && c.Mismatch < best.Mismatch {
				best = c
			}
		}
	}
	return best
}

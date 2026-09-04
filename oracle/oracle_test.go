package oracle

import (
	"bytes"
	"testing"
)

func TestSpritesDecodeSizeMasksAndFlags(t *testing.T) {
	sat := make([]uint16, 256)
	// sprite 0: y=64+10, x=32+20, pattern cell 0x47 (word 0x8E|1), 32 wide, 64 high, hflip, front, palette 5
	sat[0], sat[1], sat[2], sat[3] = 74, 52, 0x8E|1, 0x0005|0x0080|0x0100|0x0800|0x2000
	s := Sprites(sat)
	if len(s) != 64 {
		t.Fatalf("%d sprites", len(s))
	}
	got := s[0]
	want := Sprite{Index: 0, X: 20, Y: 10, Width: 32, Height: 64, Cell: 0x40, Palette: 5, HFlip: true, Front: true, SP23: true}
	if got != want {
		t.Fatalf("sprite 0 = %+v, want %+v", got, want)
	}
	if !got.Visible(256, 240) || got.Visible(10, 10) {
		t.Fatal("visibility")
	}
}

func TestTileAndSpritePixels(t *testing.T) {
	vram := make([]uint16, 0x8000)
	// tile 3, row 2: plane0 bit7 set, plane1 bit6, plane2 bit7, plane3 bit6 → x0 = 0b0101 = 5, x1 = 0b1010 = 10
	vram[3*16+2] = 0x0080 | 0x4000
	vram[3*16+2+8] = 0x0080 | 0x4000
	if p := TilePixel(vram, 3, 0, 2); p != 5 {
		t.Fatalf("tile pixel x0 = %d", p)
	}
	if p := TilePixel(vram, 3, 1, 2); p != 10 {
		t.Fatalf("tile pixel x1 = %d", p)
	}
	if p := TilePixel(vram, 0x7FF, 0, 0); p != 0 {
		t.Fatalf("out of range tile = %d", p)
	}
	// sprite cell 2, row 5: planes at +0/+16/+32/+48, bit 15 = leftmost → pixel (0,5) = 0b1001 = 9
	base := 2*64 + 5
	vram[base], vram[base+48] = 0x8000, 0x8000
	s := Sprite{Cell: 2, Width: 16, Height: 16}
	if p := SpritePixel(vram, s, 0, 5); p != 9 {
		t.Fatalf("sprite pixel = %d", p)
	}
	s.HFlip = true
	if p := SpritePixel(vram, s, 15, 5); p != 9 {
		t.Fatalf("hflipped sprite pixel = %d", p)
	}
	s.HFlip, s.VFlip = false, true
	if p := SpritePixel(vram, s, 0, 10); p != 9 {
		t.Fatalf("vflipped sprite pixel = %d", p)
	}
	if p := SpritePixel(vram, s, 16, 0); p != 0 {
		t.Fatal("outside sprite must be 0")
	}
}

func TestBATSizes(t *testing.T) {
	cases := []struct {
		mwr        uint16
		cols, rows int
	}{{0x00, 32, 32}, {0x10, 64, 32}, {0x20, 128, 32}, {0x30, 128, 32}, {0x40, 32, 64}, {0x50, 64, 64}}
	for _, c := range cases {
		cols, rows := BATSize(c.mwr)
		if cols != c.cols || rows != c.rows {
			t.Errorf("MWR %02X → %dx%d, want %dx%d", c.mwr, cols, rows, c.cols, c.rows)
		}
	}
	vram := make([]uint16, 0x8000)
	vram[33] = 0x5123
	_, _, e := BAT(vram, 0x00)
	if len(e) != 1024 || e[33] != (BATEntry{Tile: 0x123, Palette: 5}) {
		t.Fatalf("BAT entry %+v (len %d)", e[33], len(e))
	}
}

func TestChangedTilePixels(t *testing.T) {
	before := make([]uint16, 0x8000)
	after := append([]uint16(nil), before...)
	after[0x5480+3] = 0x0180 // tile 0x548, row 3: plane0 bit7 and plane1 bit0 → pixels x0 (1) and x7 (2)
	ch := ChangedTilePixels(before, after, 0x5480, 0x7920)
	if len(ch) != 2 || ch[0] != (PixelChange{Tile: 0x548, X: 0, Y: 3, After: 1}) || ch[1] != (PixelChange{Tile: 0x548, X: 7, Y: 3, After: 2}) {
		t.Fatalf("changes %+v", ch)
	}
	if n := len(ChangedTilePixels(before, after, 0x5490, 0x7920)); n != 0 {
		t.Fatalf("outside range: %d", n)
	}
}

func TestMesen2ScreenMatchAndSearch(t *testing.T) {
	// 6×4 native picture with three colours, embedded at (2,1) in a 10×6 reference.
	w, h := 6, 4
	native := make([]uint16, w*h)
	for i := range native {
		native[i] = uint16(i % 3)
	}
	rgbOf := map[uint16]uint32{0: 0x101010, 1: 0x808080, 2: 0xF0F0F0}
	var buf bytes.Buffer
	buf.WriteString("onepce-mesen2-screen-v1\t10\t6\n")
	px := make([]byte, 10*6*3)
	for y := 0; y < 6; y++ {
		for x := 0; x < 10; x++ {
			c := uint32(0x000000)
			if x >= 2 && x < 2+w && y >= 1 && y < 1+h {
				c = rgbOf[native[(y-1)*w+(x-2)]]
			}
			i := (y*10 + x) * 3
			px[i], px[i+1], px[i+2] = byte(c>>16), byte(c>>8), byte(c)
		}
	}
	buf.Write(px)
	ref, err := ReadMesen2Screen(&buf)
	if err != nil {
		t.Fatal(err)
	}
	m := MatchScreen(w, h, native, ref, 2, 1)
	if m.Mismatch != 0 || m.Compared != w*h || m.Colours != 3 {
		t.Fatalf("aligned match %+v", m)
	}
	found := SearchScreen(w, h, native, ref, 3, 2, 2)
	if found.X0 != 2 || found.Y0 != 1 || found.Mismatch != 0 {
		t.Fatalf("search %+v", found)
	}
	// Two native colours mapping to the same RGB is a mismatch, not a merge.
	native[0], native[1] = 0, 1
	ref.RGB[1*10+2], ref.RGB[1*10+3] = 0x101010, 0x101010
	if m := MatchScreen(w, h, native, ref, 2, 1); m.Mismatch == 0 {
		t.Fatal("colour-map conflict must count as a mismatch")
	}
	if _, err := ReadMesen2Screen(bytes.NewBufferString("junk\n")); err == nil {
		t.Fatal("bad header must error")
	}
	if Mesen2LeftOverscan(3) != 48 || Mesen2LeftOverscan(4) != 30 || Mesen2LeftOverscan(2) != 84 {
		t.Fatal("left overscan constants")
	}
}

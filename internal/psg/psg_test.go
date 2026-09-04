package psg

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func sel(p *PSG, ch uint8) { p.Write(0, ch) }

func TestNoiseLFSRFollowsTheTapFormula(t *testing.T) {
	// Reference sequence computed from the spec P8 formula by hand-rolled code.
	lfsr := uint32(1)
	var want []int8
	for i := 0; i < 40; i++ {
		bit := (lfsr ^ (lfsr >> 1) ^ (lfsr >> 11) ^ (lfsr >> 12) ^ (lfsr >> 17)) & 1
		out := int8(0)
		if lfsr&1 != 0 {
			out = 0x1F
		}
		want = append(want, out)
		lfsr = (lfsr >> 1) | bit<<17
	}
	p := New()
	sel(p, 4)
	p.Write(7, 0x80|0x1F) // noise on, shortest period (32 clocks)
	p.Write(4, 0x80|0x1F) // enabled, full amplitude
	var got []int8
	c := &p.Channels[4]
	for i := 0; i < 40; i++ {
		// NoiseTimer starts at 0: the first run clocks the LFSR at once.
		p.runChannel(4, c.NoiseTimer)
		got = append(got, c.NoiseOutput)
		if c.NoiseTimer != 32 {
			t.Fatalf("noise period %d, want 32", c.NoiseTimer)
		}
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("noise bit %d = %d, want %d", i, got[i], want[i])
		}
	}
	if noisePeriod(0x10) != 0x0F*64 {
		t.Fatal("noise period for f=$10")
	}
}

func TestWavetableTimingAndVolume(t *testing.T) {
	p := New()
	sel(p, 0)
	for i := 0; i < 32; i++ {
		p.Write(6, uint8(i)) // disabled: address advances after each write
	}
	if p.Channels[0].WaveAddr != 0 {
		t.Fatalf("wave address wrapped to %d", p.Channels[0].WaveAddr)
	}
	p.Write(2, 0x00)
	p.Write(3, 0x00) // period 0 → 4096
	p.Write(1, 0xFF) // master volume full both sides
	p.Write(5, 0xFF) // pan full
	p.Write(4, 0x80|0x1F)
	if p.Channels[0].Timer != 0x1000 {
		t.Fatalf("period 0 must reload 4096, got %d", p.Channels[0].Timer)
	}
	// Enabled: writes update the current cell without advancing.
	p.Write(6, 0x1F)
	if p.Channels[0].WaveAddr != 0 || p.Channels[0].WaveData[0] != 0x1F || p.Channels[0].CurrentOutput != 0x1F-OutputOffset {
		t.Fatalf("enabled wave write: addr %d data %d out %d", p.Channels[0].WaveAddr, p.Channels[0].WaveData[0], p.Channels[0].CurrentOutput)
	}
	l, r := p.Output()
	if l != int16(0x1F-OutputOffset)*255 || r != l {
		t.Fatalf("full-volume output %d/%d", l, r)
	}
	p.Write(4, 0x80|0x00) // amplitude 0 → 31 steps → muted
	if l, _ := p.Output(); l != 0 {
		t.Fatalf("muted output %d", l)
	}
	// One period of 4096 PSG clocks advances the wave address by one.
	p.Write(4, 0x80|0x1F)
	p.Advance(6 * 4096)
	if p.Channels[0].WaveAddr != 1 || p.Channels[0].Timer != 0x1000 {
		t.Fatalf("after one period: addr %d timer %d", p.Channels[0].WaveAddr, p.Channels[0].Timer)
	}
}

func TestDDAAndLFO(t *testing.T) {
	p := New()
	sel(p, 1)
	p.Write(4, 0x80|0x40|0x1F) // enabled + DDA
	p.Write(6, 0x18)
	if p.Channels[1].CurrentOutput != 0x18-OutputOffset {
		t.Fatalf("DDA output %d", p.Channels[1].CurrentOutput)
	}
	sel(p, 0)
	p.Write(2, 0x10)
	p.Write(9, 0x01) // LFO on, shift 0
	p.Write(8, 0x00) // LFO frequency 256
	if got := p.period(0); got != uint32(0x10+(0x18-OutputOffset)) {
		t.Fatalf("channel 0 period with LFO %d", got)
	}
	sel(p, 1)
	p.Write(4, 0x80|0x1F) // back to wavetable
	p.Write(2, 0x02)
	if got := p.period(1); got != 2*0x100 {
		t.Fatalf("channel 1 period with LFO %d", got)
	}
}

func TestRecorderAndWAVHeaders(t *testing.T) {
	r := NewRecorder(1, 3)
	r.Write(0, 1)  // frame 0: outside the window
	r.StartFrame() // frame 1
	r.Write(0, 2)
	r.Write(6, 3)
	r.StartFrame() // frame 2: flush frame 1
	r.Write(4, 5)
	r.StartFrame() // frame 3: flush frame 2, done
	if !r.Done() || r.Writes() != 3 {
		t.Fatalf("done %v writes %d", r.Done(), r.Writes())
	}
	b := r.Bytes()
	if string(b[:4]) != "Vgm " || binary.LittleEndian.Uint32(b[0xA4:]) != Clock || binary.LittleEndian.Uint32(b[0x18:]) != 2*735 {
		t.Fatalf("header %x", b[:0x30])
	}
	body := b[0x100:]
	want := []byte{0xB9, 0, 2, 0xB9, 6, 3, 0x62, 0xB9, 4, 5, 0x62, 0x66}
	if !bytes.Equal(body, want) {
		t.Fatalf("body %x want %x", body, want)
	}
	if binary.LittleEndian.Uint32(b[4:]) != uint32(len(b)-4) {
		t.Fatal("EOF offset")
	}
	var w bytes.Buffer
	if err := WriteWAV(&w, 44100, []int16{1, -1, 2, -2}); err != nil {
		t.Fatal(err)
	}
	h := w.Bytes()
	if string(h[:4]) != "RIFF" || binary.LittleEndian.Uint32(h[24:]) != 44100 || binary.LittleEndian.Uint32(h[40:]) != 8 || len(h) != 52 {
		t.Fatalf("wav header %x", h[:44])
	}
}

func TestSamplesFollowTheClock(t *testing.T) {
	p := New()
	p.SetSampleRate(44100)
	p.Advance(21477270) // one second of master clock
	if n := len(p.Drain()) / 2; n != 44100 {
		t.Fatalf("%d samples in one second", n)
	}
}

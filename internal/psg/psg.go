// Package psg is the HuC6280's programmable sound generator: six wavetable
// channels with DDA, noise on the last two and an LFO pairing the first two.
// Spec: docs/spec/psg.md.
//
// 參考行為：Mesen2 PcePsg.cpp／PcePsgChannel.cpp @ b9fa69d（只取行為事實：時脈、
// 計時器排程、雜訊 LFSR、音量表、寫入副作用）。結構與取樣輸出照本 repo 的裝置慣例。
package psg

// Clock is the PSG clock: master clock / 6.
const Clock = 21477270 / 6

// OutputOffset is subtracted from every 5-bit sample (HuC6280A behaviour,
// the oracle's default).
const OutputOffset = 0x10

// Channel is one voice, fields named after the oracle's state so snapshots
// can be compared key by key (spec §6).
type Channel struct {
	Frequency      uint16
	Amplitude      uint8
	Enabled        bool
	LeftVolume     uint8
	RightVolume    uint8
	WaveData       [32]uint8
	DDAEnabled     bool
	DDAOutputValue uint8
	WaveAddr       uint8
	Timer          uint32
	CurrentOutput  int8
	NoiseLFSR      uint32
	NoiseTimer     uint32
	NoiseEnabled   bool
	NoiseOutput    int8
	NoiseFrequency uint8
}

// State is the whole chip.
type State struct {
	ChannelSelect uint8
	LeftVolume    uint8
	RightVolume   uint8
	LFOFrequency  uint8
	LFOControl    uint8
	Channels      [6]Channel
}

// PSG is the chip plus its audio output.
type PSG struct {
	State
	lastWrite State // spec P14: the oracle's view at its last write

	lastMaster uint64 // master clock the chip has been run to (multiple of 6 left over)
	remainder  uint32 // master clocks not yet forming a full PSG clock

	// Audio: zero-order-hold sampling of the mixed output.
	sampleRate int
	sampleAcc  uint64
	outL, outR int16
	samples    []int16

	// OnWrite, when set, sees every port write (VGM recording).
	OnWrite func(port, value uint8)
	// Now, when set, returns the current master clock: the chip runs itself
	// up to now before applying a write (spec P1: the oracle runs the PSG
	// lazily, and the noise LFSR phase depends on that cadence).
	Now func() uint64
}

// New returns a chip in its reset state (noise LFSRs at 1, everything else 0).
func New() *PSG {
	p := &PSG{}
	for i := range p.Channels {
		p.Channels[i].NoiseLFSR = 1
	}
	p.lastWrite = p.State
	return p
}

// SetSampleRate turns sample generation on (0 turns it off).
func (p *PSG) SetSampleRate(rate int) { p.sampleRate = rate; p.sampleAcc = 0 }

// Drain returns the interleaved stereo samples produced so far and clears them.
func (p *PSG) Drain() []int16 {
	s := p.samples
	p.samples = nil
	return s
}

// LastWriteState is the chip as it was right after the most recent port
// write (spec P14).
func (p *PSG) LastWriteState() State { return p.lastWrite }

// --- bus.Device ---

// Read: the PSG has no readable registers; the bus returns its I/O buffer.
func (p *PSG) Read(port uint16) uint8 { return 0xFF }

// Write runs the chip up to now, then applies a port write.
func (p *PSG) Write(port uint16, value uint8) {
	if p.Now != nil {
		p.Advance(p.Now())
	}
	if p.OnWrite != nil {
		p.OnWrite(uint8(port&0x0F), value)
	}
	switch port & 0x0F {
	case 0:
		p.ChannelSelect = value & 0x07
	case 1:
		p.RightVolume = value & 0x0F
		p.LeftVolume = (value >> 4) & 0x0F
	case 2, 3, 4, 5, 6:
		if p.ChannelSelect < 6 {
			p.writeChannel(int(p.ChannelSelect), uint8(port&0x0F), value)
		}
	case 7:
		if p.ChannelSelect == 4 || p.ChannelSelect == 5 {
			p.writeChannel(int(p.ChannelSelect), 7, value)
		}
	case 8:
		p.LFOFrequency = value
	case 9:
		p.LFOControl = value
	}
	p.updateOutput()
	p.lastWrite = p.State
}

func (p *PSG) writeChannel(i int, port uint8, value uint8) {
	c := &p.Channels[i]
	switch port {
	case 2:
		c.Frequency = c.Frequency&0xF00 | uint16(value)
	case 3:
		c.Frequency = c.Frequency&0x0FF | uint16(value&0x0F)<<8
	case 4:
		enabled := value&0x80 != 0
		if c.Enabled != enabled {
			c.Timer = p.period(i)
			c.Enabled = enabled
		}
		c.DDAEnabled = value&0x40 != 0
		c.Amplitude = value & 0x1F
		if c.DDAEnabled {
			if c.Enabled {
				c.CurrentOutput = int8(c.DDAOutputValue) - OutputOffset
			} else {
				c.WaveAddr = 0
			}
		}
	case 5:
		c.RightVolume = value & 0x0F
		c.LeftVolume = (value >> 4) & 0x0F
	case 6:
		if c.DDAEnabled {
			c.DDAOutputValue = value & 0x1F
			if c.Enabled {
				c.CurrentOutput = int8(c.DDAOutputValue) - OutputOffset
			}
		} else {
			c.WaveData[c.WaveAddr] = value & 0x1F
			if !c.Enabled {
				c.WaveAddr = (c.WaveAddr + 1) & 0x1F
			}
			if !c.NoiseEnabled {
				c.CurrentOutput = int8(c.WaveData[c.WaveAddr]) - OutputOffset
			}
		}
	case 7:
		c.NoiseEnabled = value&0x80 != 0
		c.NoiseFrequency = value & 0x1F
	}
}

// --- timing ---

func (p *PSG) lfoEnabled() bool { return p.LFOControl&0x80 == 0 && p.LFOControl&0x03 != 0 }

func (p *PSG) lfoFrequency() uint32 {
	if p.LFOFrequency == 0 {
		return 0x100
	}
	return uint32(p.LFOFrequency)
}

// period is the channel's effective period in PSG clocks (spec P3/P10).
func (p *PSG) period(i int) uint32 {
	period := uint32(p.Channels[i].Frequency)
	if i == 0 && p.lfoEnabled() {
		shift := (uint(p.LFOControl&0x03) - 1) * 2
		period = (period + uint32(int32(p.Channels[1].CurrentOutput)<<shift)) & 0xFFF
	}
	if period == 0 {
		period = 0x1000
	}
	if i == 1 && p.lfoEnabled() {
		period *= p.lfoFrequency()
	}
	return period
}

func noisePeriod(f uint8) uint32 {
	if f == 0x1F {
		return 32
	}
	return uint32((^f)&0x1F) * 64
}

// timerFor is the number of PSG clocks until the channel next needs
// attention (0 = none), the oracle's GetTimer.
func (p *PSG) timerFor(i int) uint32 {
	c := &p.Channels[i]
	var min uint32
	if i >= 4 {
		min = c.NoiseTimer
	}
	if c.Enabled && !c.DDAEnabled && (min == 0 || c.Timer < min) {
		return c.Timer
	}
	return min
}

func (p *PSG) runChannel(i int, clocks uint32) {
	c := &p.Channels[i]
	if i >= 4 {
		if c.NoiseTimer <= clocks {
			c.NoiseTimer = noisePeriod(c.NoiseFrequency)
			v := c.NoiseLFSR
			bit := (v ^ (v >> 1) ^ (v >> 11) ^ (v >> 12) ^ (v >> 17)) & 1
			if c.NoiseLFSR&1 != 0 {
				c.NoiseOutput = 0x1F
			} else {
				c.NoiseOutput = 0
			}
			c.NoiseLFSR = (c.NoiseLFSR >> 1) | bit<<17
		} else {
			c.NoiseTimer -= clocks
		}
	}
	switch {
	case !c.Enabled:
		c.CurrentOutput = 0
	case c.DDAEnabled:
		c.CurrentOutput = int8(c.DDAOutputValue) - OutputOffset
	case !c.NoiseEnabled:
		c.Timer -= clocks
		if c.Timer == 0 {
			c.Timer = p.period(i)
			c.WaveAddr = (c.WaveAddr + 1) & 0x1F
		}
		c.CurrentOutput = int8(c.WaveData[c.WaveAddr]) - OutputOffset
	default:
		c.CurrentOutput = c.NoiseOutput - OutputOffset
	}
}

// Advance runs the chip up to master clock `master` (spec P1).
func (p *PSG) Advance(master uint64) {
	if master <= p.lastMaster {
		return
	}
	clocksToRun := uint32(master-p.lastMaster) + p.remainder
	for clocksToRun >= 6 {
		minTimer := clocksToRun / 6
		for i := 0; i < 6; i++ {
			if t := p.timerFor(i); t != 0 && t < minTimer {
				minTimer = t
			}
		}
		p.emit(minTimer)
		for i := 0; i < 6; i++ {
			p.runChannel(i, minTimer)
		}
		clocksToRun -= minTimer * 6
		p.updateOutput()
	}
	p.remainder = clocksToRun
	p.lastMaster = master
}

// emit produces the samples that fall inside the next `clocks` PSG clocks,
// all holding the current output (spec P12).
func (p *PSG) emit(clocks uint32) {
	if p.sampleRate <= 0 {
		return
	}
	p.sampleAcc += uint64(clocks) * uint64(p.sampleRate)
	for p.sampleAcc >= Clock {
		p.sampleAcc -= Clock
		p.samples = append(p.samples, p.outL, p.outR)
	}
}

var volumeReduction = [30]int16{255, 214, 180, 151, 127, 107, 90, 76, 64, 53, 45, 38, 32, 27, 22, 19, 16, 13, 11, 9, 8, 6, 5, 4, 4, 3, 2, 2, 2, 1}

// channelOutput is one channel's contribution to one side (spec P11).
func (p *PSG) channelOutput(i int, left bool) int16 {
	c := &p.Channels[i]
	master, pan := p.RightVolume, c.RightVolume
	if left {
		master, pan = p.LeftVolume, c.LeftVolume
	}
	steps := int(0xF-master)*2 + int(0x1F-c.Amplitude) + int(0xF-pan)*2
	if steps >= 30 {
		return 0
	}
	return int16(c.CurrentOutput) * volumeReduction[steps]
}

func (p *PSG) updateOutput() {
	var l, r int16
	for i := 0; i < 6; i++ {
		l += p.channelOutput(i, true)
		r += p.channelOutput(i, false)
	}
	p.outL, p.outR = l, r
}

// Output is the current mixed output (left, right).
func (p *PSG) Output() (int16, int16) { return p.outL, p.outR }

// Save/Load for savestates: the public State plus the run position.
type Saved struct {
	State      State
	LastWrite  State
	LastMaster uint64
	Remainder  uint32
	OutL, OutR int16
}

func (p *PSG) Save() Saved {
	return Saved{State: p.State, LastWrite: p.lastWrite, LastMaster: p.lastMaster, Remainder: p.remainder, OutL: p.outL, OutR: p.outR}
}

func (p *PSG) Load(s Saved) {
	p.State, p.lastWrite, p.lastMaster, p.remainder, p.outL, p.outR = s.State, s.LastWrite, s.LastMaster, s.Remainder, s.OutL, s.OutR
	p.samples, p.sampleAcc = nil, 0
}

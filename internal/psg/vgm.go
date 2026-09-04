package psg

import "encoding/binary"

// Recorder writes port writes into a frame-synchronised VGM stream in the
// same shape as nectaris-cht/tools/mesen2_pce_psg_vgm_probe.lua (spec §3):
// 0xB9 port data per write, one 0x62 (735 samples) per frame boundary.
type Recorder struct {
	start, stop uint64 // frame window [start, stop) for writes
	frame       uint64 // frame counter advanced at every StartFrame
	pending     [][2]uint8
	commands    []byte
	frames      int
	writes      int
	done        bool
}

// NewRecorder records writes made while the frame counter is in
// [start, stop); the counter starts at 0 and advances on StartFrame.
func NewRecorder(start, stop uint64) *Recorder {
	return &Recorder{start: start, stop: stop}
}

// Write queues one port write if the window is open.
func (r *Recorder) Write(port, value uint8) {
	if r.frame < r.start || r.frame >= r.stop {
		return
	}
	r.pending = append(r.pending, [2]uint8{port, value})
	r.writes++
}

// StartFrame marks a frame boundary: flush the frame's writes and a wait.
func (r *Recorder) StartFrame() {
	r.frame++
	if r.frame > r.start && r.frame <= r.stop {
		for _, w := range r.pending {
			r.commands = append(r.commands, 0xB9, w[0], w[1])
		}
		r.commands = append(r.commands, 0x62)
		r.frames++
	}
	r.pending = r.pending[:0]
	if r.frame >= r.stop {
		r.done = true
	}
}

// Done reports whether the window has closed.
func (r *Recorder) Done() bool { return r.done }

// Writes is the number of port writes recorded so far.
func (r *Recorder) Writes() int { return r.writes }

// Bytes renders the VGM file (version 1.71, HuC6280 clock at 0xA4).
func (r *Recorder) Bytes() []byte {
	body := append(append([]byte(nil), r.commands...), 0x66)
	head := make([]byte, 0x100)
	copy(head, "Vgm ")
	binary.LittleEndian.PutUint32(head[0x04:], uint32(0x100+len(body)-4))
	binary.LittleEndian.PutUint32(head[0x08:], 0x00000171)
	binary.LittleEndian.PutUint32(head[0x18:], uint32(r.frames*735))
	binary.LittleEndian.PutUint32(head[0x24:], 60)
	binary.LittleEndian.PutUint32(head[0x34:], 0x100-0x34)
	binary.LittleEndian.PutUint32(head[0xA4:], Clock)
	return append(head, body...)
}

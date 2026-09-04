package psg

import (
	"encoding/binary"
	"io"
)

// WriteWAV writes interleaved stereo 16-bit PCM as a RIFF/WAVE file.
func WriteWAV(w io.Writer, rate int, samples []int16) error {
	data := len(samples) * 2
	head := make([]byte, 44)
	copy(head, "RIFF")
	binary.LittleEndian.PutUint32(head[4:], uint32(36+data))
	copy(head[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(head[16:], 16)
	binary.LittleEndian.PutUint16(head[20:], 1) // PCM
	binary.LittleEndian.PutUint16(head[22:], 2) // channels
	binary.LittleEndian.PutUint32(head[24:], uint32(rate))
	binary.LittleEndian.PutUint32(head[28:], uint32(rate*4))
	binary.LittleEndian.PutUint16(head[32:], 4)
	binary.LittleEndian.PutUint16(head[34:], 16)
	copy(head[36:], "data")
	binary.LittleEndian.PutUint32(head[40:], uint32(data))
	if _, err := w.Write(head); err != nil {
		return err
	}
	buf := make([]byte, data)
	for i, s := range samples {
		binary.LittleEndian.PutUint16(buf[2*i:], uint16(s))
	}
	_, err := w.Write(buf)
	return err
}

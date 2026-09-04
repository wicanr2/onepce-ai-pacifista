// Package gui is the display-independent core of the comparison GUI
// (docs/spec/gui.md): it drives a public onepce.Machine, records input as a
// frame plan, keeps the recent watch hits and compares the native picture
// with reference images. cmd/onepce-gui puts an Ebiten window around it.
package gui

import (
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/wicanr2/onepce-ai-remake"
)

// MaxHits is how many recent watch events the session keeps.
const MaxHits = 200

// Session drives one machine one frame per GUI tick (spec G2).
type Session struct {
	M      *onepce.Machine
	Paused bool

	hits    []onepce.Event
	watches []*onepce.Watch

	held      uint8
	heldSince [8]uint64 // frame at which the bit was scheduled first
	recorded  []onepce.Press
	scheduled []onepce.Press
}

// New wraps a loaded machine.
func New(m *onepce.Machine) *Session { return &Session{M: m} }

// Schedule adds presses (from -press) that count as part of the plan.
func (s *Session) Schedule(presses ...onepce.Press) {
	s.scheduled = append(s.scheduled, presses...)
	s.M.Schedule(presses...)
}

// Tick is one GUI frame: input first, then one machine frame unless paused
// (spec G2/G3). held is the pad state as button bits.
func (s *Session) Tick(held uint8) {
	s.applyInput(held)
	if !s.Paused {
		s.M.RunFrames(1)
	}
}

// applyInput schedules newly pressed buttons for the next frame boundary
// and closes the presses of released ones, merging holds (spec G3).
func (s *Session) applyInput(held uint8) {
	next := s.M.Frame() + 1
	for bit := 0; bit < 8; bit++ {
		mask := uint8(1 << bit)
		now, before := held&mask != 0, s.held&mask != 0
		switch {
		case now && !before:
			s.heldSince[bit] = next
		case !now && before:
			span := int(next - s.heldSince[bit])
			if span > 0 {
				s.recorded = append(s.recorded, onepce.Press{Frame: s.heldSince[bit], Button: mask, Span: span})
			}
		}
		if now && !s.Paused {
			s.M.Schedule(onepce.Press{Frame: next, Button: mask, Span: 1})
		}
	}
	s.held = held
}

// StepInstruction runs one instruction (pausing the session).
func (s *Session) StepInstruction() {
	s.Paused = true
	s.M.Step()
}

// StepFrame runs one frame (pausing the session).
func (s *Session) StepFrame() {
	s.Paused = true
	s.M.RunFrames(1)
}

// Plan is everything scheduled plus everything recorded so far, buttons
// still held closed at the current frame, sorted by frame.
func (s *Session) Plan() []onepce.Press {
	out := append([]onepce.Press(nil), s.scheduled...)
	out = append(out, s.recorded...)
	next := s.M.Frame() + 1
	for bit := 0; bit < 8; bit++ {
		if s.held&(1<<bit) != 0 {
			if span := int(next - s.heldSince[bit]); span > 0 {
				out = append(out, onepce.Press{Frame: s.heldSince[bit], Button: 1 << bit, Span: span})
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Frame < out[j].Frame })
	return out
}

// FormatPresses renders a plan in the "frame:button:span,…" form.
func FormatPresses(plan []onepce.Press) string {
	parts := make([]string, 0, len(plan))
	for _, p := range plan {
		parts = append(parts, fmt.Sprintf("%d:%s:%d", p.Frame, onepce.ButtonName(p.Button), p.Span))
	}
	return strings.Join(parts, ",")
}

// Watch installs a watch in the CLI's kind:space:lo-hi[:limit] syntax; hits
// land in Hits.
func (s *Session) Watch(spec string) error {
	kind, space, lo, hi, limit, err := ParseWatch(spec)
	if err != nil {
		return err
	}
	w := s.M.Watch(kind, space, lo, hi, func(e onepce.Event) {
		s.hits = append(s.hits, e)
		if len(s.hits) > MaxHits {
			s.hits = s.hits[len(s.hits)-MaxHits:]
		}
	})
	if limit > 0 {
		w.Limit(limit)
	}
	s.watches = append(s.watches, w)
	return nil
}

// Hits returns the recent watch events, oldest first.
func (s *Session) Hits() []onepce.Event { return s.hits }

// Watches returns the installed watches (for their counters).
func (s *Session) Watches() []*onepce.Watch { return s.watches }

// ParseWatch parses kind:space:lo-hi[:limit] (shared with cmd/onepce).
func ParseWatch(spec string) (onepce.Kind, onepce.Space, uint32, uint32, int, error) {
	parts := strings.Split(spec, ":")
	if len(parts) < 3 || len(parts) > 4 {
		return 0, 0, 0, 0, 0, fmt.Errorf("watch %q: want kind:space:lo-hi[:limit]", spec)
	}
	var kind onepce.Kind
	switch parts[0] {
	case "read":
		kind = onepce.Read
	case "write":
		kind = onepce.Write
	case "exec":
		kind = onepce.Exec
	default:
		return 0, 0, 0, 0, 0, fmt.Errorf("watch %q: kind must be read|write|exec", spec)
	}
	var space onepce.Space
	switch parts[1] {
	case "cpu":
		space = onepce.CPU
	case "vram":
		space = onepce.VRAM
	default:
		return 0, 0, 0, 0, 0, fmt.Errorf("watch %q: space must be cpu|vram", spec)
	}
	rng := strings.SplitN(parts[2], "-", 2)
	lo, err := strconv.ParseUint(strings.TrimPrefix(rng[0], "$"), 16, 32)
	if err != nil {
		return 0, 0, 0, 0, 0, fmt.Errorf("watch %q: lo: %w", spec, err)
	}
	hi := lo
	if len(rng) == 2 {
		if hi, err = strconv.ParseUint(strings.TrimPrefix(rng[1], "$"), 16, 32); err != nil {
			return 0, 0, 0, 0, 0, fmt.Errorf("watch %q: hi: %w", spec, err)
		}
	}
	limit := 0
	if len(parts) == 4 {
		if limit, err = strconv.Atoi(parts[3]); err != nil {
			return 0, 0, 0, 0, 0, fmt.Errorf("watch %q: limit: %w", spec, err)
		}
	}
	return kind, space, uint32(lo), uint32(hi), limit, nil
}

// --- reference images (spec G4) ---

// Reference is one PNG or a frame sequence of PNGs.
type Reference struct {
	Paths []string
	Start uint64 // machine frame shown by Paths[0]
	Every int    // machine frames per reference frame
	cache map[int]image.Image
}

// LoadReference opens a PNG file or a directory of PNGs (sorted by name).
func LoadReference(path string, start uint64, every int) (*Reference, error) {
	if every <= 0 {
		every = 1
	}
	r := &Reference{Start: start, Every: every, cache: map[int]image.Image{}}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		entries, err := os.ReadDir(path)
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(strings.ToLower(e.Name()), ".png") {
				r.Paths = append(r.Paths, filepath.Join(path, e.Name()))
			}
		}
		sort.Strings(r.Paths)
		if len(r.Paths) == 0 {
			return nil, fmt.Errorf("%s: no PNG files", path)
		}
	} else {
		r.Paths = []string{path}
	}
	return r, nil
}

// Index maps a machine frame to a sequence index (clamped).
func (r *Reference) Index(frame uint64) int {
	if frame < r.Start {
		return 0
	}
	i := int((frame - r.Start) / uint64(r.Every))
	if i >= len(r.Paths) {
		i = len(r.Paths) - 1
	}
	return i
}

// At returns the image for a machine frame (decoded on demand).
func (r *Reference) At(frame uint64) (image.Image, error) { return r.Image(r.Index(frame)) }

// Image returns the i-th image of the sequence.
func (r *Reference) Image(i int) (image.Image, error) {
	if i < 0 || i >= len(r.Paths) {
		return nil, fmt.Errorf("reference index %d out of range", i)
	}
	if img, ok := r.cache[i]; ok {
		return img, nil
	}
	f, err := os.Open(r.Paths[i])
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", r.Paths[i], err)
	}
	if len(r.cache) > 64 {
		r.cache = map[int]image.Image{}
	}
	r.cache[i] = img
	return img, nil
}

// --- comparison (spec G5) ---

// Tolerance is the per-channel difference (0–255) still counted as equal.
const Tolerance = 8

// Diff compares the native picture with a reference canvas mapped by
// (scale, ox, oy): native (x, y) is read at canvas (ox + x*scale + scale/2,
// oy + y*scale + scale/2). The result is the native picture with differing
// pixels painted magenta; differing is their count. Pixels outside the
// reference are left alone and not counted.
func Diff(native *image.RGBA, ref image.Image, scale, ox, oy int) (*image.RGBA, int) {
	if scale <= 0 {
		scale = 1
	}
	b := native.Bounds()
	out := image.NewRGBA(b)
	copy(out.Pix, native.Pix)
	rb := ref.Bounds()
	differing := 0
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			cx, cy := ox+x*scale+scale/2, oy+y*scale+scale/2
			if cx < rb.Min.X || cx >= rb.Max.X || cy < rb.Min.Y || cy >= rb.Max.Y {
				continue
			}
			nr, ng, nb, _ := native.At(x, y).RGBA()
			rr, rg, rbb, _ := ref.At(cx, cy).RGBA()
			if absDiff(nr>>8, rr>>8) > Tolerance || absDiff(ng>>8, rg>>8) > Tolerance || absDiff(nb>>8, rbb>>8) > Tolerance {
				differing++
				i := out.PixOffset(x, y)
				out.Pix[i], out.Pix[i+1], out.Pix[i+2], out.Pix[i+3] = 255, 0, 255, 255
			}
		}
	}
	return out, differing
}

func absDiff(a, b uint32) uint32 {
	if a > b {
		return a - b
	}
	return b - a
}

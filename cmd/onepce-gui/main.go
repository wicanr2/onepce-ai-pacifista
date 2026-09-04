// Command onepce-gui is the comparison window (docs/spec/gui.md): the
// emulator's native picture next to a remake screenshot or frame sequence,
// with pause/step, watch hits and input recording. It only talks to the
// public onepce API through internal/gui.
package main

import (
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/ebitenutil"
	"github.com/hajimehoshi/ebiten/v2/inpututil"

	"github.com/wicanr2/onepce-ai-remake"
	"github.com/wicanr2/onepce-ai-remake/internal/gui"
)

type watchFlags []string

func (w *watchFlags) String() string     { return strings.Join(*w, " ") }
func (w *watchFlags) Set(v string) error { *w = append(*w, v); return nil }

const (
	modeSide = iota
	modeOverlay
	modeDiff
)

type app struct {
	s          *gui.Session
	ref        *gui.Reference
	refOffset  int // manual index shift ([ and ])
	scale      int
	refScale   int
	refOX      int
	refOY      int
	mode       int
	recordPlan string
	shotDir    string
	native     *ebiten.Image
	right      *ebiten.Image
	status     string
	differing  int
	hudH       int
}

func main() {
	romPath := flag.String("rom", "", "HuCard image (.pce)")
	press := flag.String("press", "", "input plan: frame:button:span,…")
	refPath := flag.String("ref", "", "reference PNG or directory of PNGs (nectaris -record-dir)")
	refStart := flag.Uint64("ref-start", 0, "machine frame shown by the first reference image")
	refEvery := flag.Int("ref-every", 1, "machine frames per reference image")
	refScale := flag.Int("ref-scale", 3, "reference canvas pixels per native pixel")
	refOffset := flag.String("ref-offset", "96,0", "reference canvas origin of native (0,0): x,y")
	scale := flag.Int("scale", 2, "window pixels per native pixel")
	recordPlan := flag.String("record-plan", "", "write the scheduled+recorded input plan here on exit")
	shotDir := flag.String("out", ".", "directory for screenshots (C) and snapshots (S)")
	var watches watchFlags
	flag.Var(&watches, "watch", "kind:space:lo-hi[:limit] (repeatable)")
	flag.Parse()
	if *romPath == "" {
		fmt.Fprintln(os.Stderr, "onepce-gui: -rom is required")
		os.Exit(2)
	}
	rom, err := os.ReadFile(*romPath)
	if err != nil {
		fatal(err)
	}
	m, err := onepce.Load(rom)
	if err != nil {
		fatal(err)
	}
	a := &app{s: gui.New(m), scale: *scale, refScale: *refScale, recordPlan: *recordPlan, shotDir: *shotDir, hudH: 96}
	if _, err := fmt.Sscanf(*refOffset, "%d,%d", &a.refOX, &a.refOY); err != nil {
		fatal(fmt.Errorf("-ref-offset %q: want x,y", *refOffset))
	}
	presses, err := onepce.ParsePresses(*press)
	if err != nil {
		fatal(err)
	}
	a.s.Schedule(presses...)
	for _, spec := range watches {
		if err := a.s.Watch(spec); err != nil {
			fatal(err)
		}
	}
	if *refPath != "" {
		if a.ref, err = gui.LoadReference(*refPath, *refStart, *refEvery); err != nil {
			fatal(err)
		}
	}
	w, h, _ := m.FramebufferNative()
	if w == 0 || h == 0 {
		w, h = 256, 240
	}
	ebiten.SetWindowSize(w*a.scale*2+8, h*a.scale+a.hudH)
	ebiten.SetWindowTitle("onepce " + onepce.Version)
	if err := ebiten.RunGame(a); err != nil && err != ebiten.Termination {
		fatal(err)
	}
	if a.recordPlan != "" {
		plan := gui.FormatPresses(a.s.Plan())
		if err := os.WriteFile(a.recordPlan, []byte(plan+"\n"), 0o644); err != nil {
			fatal(err)
		}
		fmt.Fprintf(os.Stderr, "plan written to %s (%d presses)\n", a.recordPlan, len(a.s.Plan()))
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "onepce-gui:", err)
	os.Exit(1)
}

func heldButtons() uint8 {
	var held uint8
	for key, bit := range map[ebiten.Key]uint8{
		ebiten.KeyArrowUp: onepce.ButtonUp, ebiten.KeyArrowDown: onepce.ButtonDown,
		ebiten.KeyArrowLeft: onepce.ButtonLeft, ebiten.KeyArrowRight: onepce.ButtonRight,
		ebiten.KeyZ: onepce.ButtonI, ebiten.KeyX: onepce.ButtonII,
		ebiten.KeyEnter: onepce.ButtonRun, ebiten.KeyShiftRight: onepce.ButtonSelect,
	} {
		if ebiten.IsKeyPressed(key) {
			held |= bit
		}
	}
	return held
}

func (a *app) Update() error {
	if inpututil.IsKeyJustPressed(ebiten.KeyP) {
		a.s.Paused = !a.s.Paused
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyN) {
		a.s.StepInstruction()
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyF) {
		a.s.StepFrame()
	}
	if inpututil.IsKeyJustPressed(ebiten.Key1) {
		a.mode = modeSide
	}
	if inpututil.IsKeyJustPressed(ebiten.Key2) {
		a.mode = modeOverlay
	}
	if inpututil.IsKeyJustPressed(ebiten.Key3) {
		a.mode = modeDiff
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyBracketLeft) {
		a.refOffset--
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyBracketRight) {
		a.refOffset++
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyC) {
		a.screenshot()
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyS) {
		a.snapshot()
	}
	a.s.Tick(heldButtons())
	a.refresh()
	return nil
}

func (a *app) refresh() {
	fb := a.s.M.Framebuffer()
	if fb.Bounds().Dx() == 0 || fb.Bounds().Dy() == 0 {
		// Nothing rendered yet (before the first display line of power-on).
		return
	}
	a.native = ebiten.NewImageFromImage(fb)
	a.right = nil
	a.differing = 0
	if a.ref == nil {
		return
	}
	idx := a.ref.Index(a.s.M.Frame()) + a.refOffset
	if idx < 0 {
		idx = 0
	}
	if idx >= len(a.ref.Paths) {
		idx = len(a.ref.Paths) - 1
	}
	img, err := a.ref.Image(idx)
	if err != nil {
		a.status = err.Error()
		return
	}
	switch a.mode {
	case modeDiff:
		out, n := gui.Diff(fb, img, a.refScale, a.refOX, a.refOY)
		a.differing = n
		a.right = ebiten.NewImageFromImage(out)
	default:
		a.right = ebiten.NewImageFromImage(img)
	}
	a.status = fmt.Sprintf("ref %d/%d %s", idx+1, len(a.ref.Paths), filepath.Base(a.ref.Paths[idx]))
}

func (a *app) Draw(screen *ebiten.Image) {
	screen.Fill(color.RGBA{R: 24, G: 24, B: 28, A: 255})
	if a.native == nil {
		return
	}
	w, h := a.native.Bounds().Dx(), a.native.Bounds().Dy()
	op := &ebiten.DrawImageOptions{}
	op.GeoM.Scale(float64(a.scale), float64(a.scale))
	screen.DrawImage(a.native, op)
	if a.right != nil {
		rop := &ebiten.DrawImageOptions{}
		rw, rh := a.right.Bounds().Dx(), a.right.Bounds().Dy()
		switch a.mode {
		case modeOverlay:
			// The reference canvas mapped onto the native picture, half transparent.
			s := float64(a.scale) / float64(a.refScale)
			rop.GeoM.Scale(s, s)
			rop.GeoM.Translate(-float64(a.refOX)*s, -float64(a.refOY)*s)
			rop.ColorScale.ScaleAlpha(0.5)
			screen.DrawImage(a.right, rop)
		default:
			sx := float64(w*a.scale) / float64(rw)
			sy := float64(h*a.scale) / float64(rh)
			if sx > sy {
				sx = sy
			}
			rop.GeoM.Scale(sx, sx)
			rop.GeoM.Translate(float64(w*a.scale+8), 0)
			screen.DrawImage(a.right, rop)
		}
	}
	a.drawHUD(screen, h*a.scale)
}

func (a *app) drawHUD(screen *ebiten.Image, y int) {
	m := a.s.M
	regs := m.Registers()
	r := m.Internal().VDC.Registers()
	state := "run"
	if a.s.Paused {
		state = "PAUSED"
	}
	modes := []string{"side", "overlay", "diff"}
	lines := []string{
		fmt.Sprintf("%s  frame %d  scanline %d  hclock %d  pc %s  [%s]", state, m.Frame(), r.Scanline, r.HClock, m.Resolve(regs.PC), modes[a.mode]),
		"P pause  N step  F frame  C shot  S snapshot  1/2/3 mode  [ ] ref  arrows/Z/X/Enter/RShift = pad",
	}
	if a.status != "" {
		lines = append(lines, a.status)
	}
	if a.mode == modeDiff && a.right != nil {
		lines = append(lines, fmt.Sprintf("differing pixels: %d", a.differing))
	}
	for i, w := range a.s.Watches() {
		lines = append(lines, fmt.Sprintf("watch %d: count %d skipped %d", i, w.Count(), w.Skipped()))
	}
	hits := a.s.Hits()
	for i := len(hits) - 1; i >= 0 && i >= len(hits)-4; i-- {
		e := hits[i]
		lines = append(lines, fmt.Sprintf("  f%d sl%d pc %04X %s <- %04X", e.Frame, e.Scanline, e.PC, e.Addr, e.Value))
	}
	ebitenutil.DebugPrintAt(screen, strings.Join(lines, "\n"), 4, y+2)
}

func (a *app) Layout(outsideWidth, outsideHeight int) (int, int) {
	w, h := 256, 240
	if a.native != nil {
		w, h = a.native.Bounds().Dx(), a.native.Bounds().Dy()
	}
	return w*a.scale*2 + 8, h*a.scale + a.hudH
}

func (a *app) screenshot() {
	path := filepath.Join(a.shotDir, fmt.Sprintf("onepce-frame-%d.png", a.s.M.Frame()))
	f, err := os.Create(path)
	if err != nil {
		a.status = err.Error()
		return
	}
	defer f.Close()
	if err := png.Encode(f, a.s.M.Framebuffer()); err != nil {
		a.status = err.Error()
		return
	}
	a.status = "wrote " + path
}

func (a *app) snapshot() {
	dir := filepath.Join(a.shotDir, fmt.Sprintf("onepce-snapshot-%d", a.s.M.Frame()))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		a.status = err.Error()
		return
	}
	snap := a.s.M.Snapshot()
	if err := os.WriteFile(filepath.Join(dir, "ram.bin"), snap.RAM, 0o644); err != nil {
		a.status = err.Error()
		return
	}
	for name, words := range map[string][]uint16{"vram.bin": snap.VRAM, "sat.bin": snap.SAT, "palette.bin": snap.VCE} {
		b := make([]byte, 2*len(words))
		for i, v := range words {
			b[2*i], b[2*i+1] = byte(v), byte(v>>8)
		}
		if err := os.WriteFile(filepath.Join(dir, name), b, 0o644); err != nil {
			a.status = err.Error()
			return
		}
	}
	a.status = "wrote " + dir
}

var _ image.Image = (*image.RGBA)(nil)

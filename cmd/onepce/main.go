// Command onepce is the command-line face of the emulator: scripted runs
// with watches, screenshots, snapshots and savestates, or a JSON-RPC server
// over stdin/stdout. Spec: docs/spec/cli-rpc.md.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/wicanr2/onepce-ai-pacifista"
	"github.com/wicanr2/onepce-ai-pacifista/internal/psg"
	"github.com/wicanr2/onepce-ai-pacifista/internal/rpc"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "run":
		err = runCommand(os.Args[2:])
	case "rpc":
		err = rpcCommand(os.Args[2:])
	case "version":
		fmt.Println(onepce.Version)
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "onepce:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `usage:
  onepce run -rom X [-press "f:btn:span,…"] [-to-frame N] [-load in.state] [-save out.state]
             [-screenshot out.png] [-snapshot-dir DIR] [-watch "kind:space:lo-hi[:limit]"]…
             [-ignore-pc "a,b,…"] [-trace-hash] [-wav out.wav] [-audio-rate 44100]
             [-vgm start-stop -vgm-out out.vgm]
  onepce rpc -rom X
  onepce version`)
}

type watchFlags []string

func (w *watchFlags) String() string     { return strings.Join(*w, " ") }
func (w *watchFlags) Set(v string) error { *w = append(*w, v); return nil }

func load(romPath string) (*onepce.Machine, error) {
	if romPath == "" {
		return nil, fmt.Errorf("-rom is required")
	}
	rom, err := os.ReadFile(romPath)
	if err != nil {
		return nil, err
	}
	return onepce.Load(rom)
}

func runCommand(args []string) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	romPath := fs.String("rom", "", "HuCard image (.pce)")
	press := fs.String("press", "", "input plan: frame:button:span,…")
	toFrame := fs.Uint64("to-frame", 0, "run until the frame counter reaches this")
	loadPath := fs.String("load", "", "savestate to start from")
	savePath := fs.String("save", "", "savestate to write at the end")
	shot := fs.String("screenshot", "", "PNG of the display window at the end")
	snapDir := fs.String("snapshot-dir", "", "directory for snapshot.json + section bins")
	ignore := fs.String("ignore-pc", "", "instruction-start PCs to ignore in watches (hex, comma separated)")
	traceHash := fs.Bool("trace-hash", false, "print the PC+opcode structure hash of the run")
	wavPath := fs.String("wav", "", "render the PSG output of the run to this WAV file")
	audioRate := fs.Int("audio-rate", 44100, "sample rate for -wav")
	vgmWindow := fs.String("vgm", "", "record PSG port writes while the start-of-frame counter is in start-stop")
	vgmPath := fs.String("vgm-out", "", "VGM file for -vgm")
	var watches watchFlags
	fs.Var(&watches, "watch", "kind:space:lo-hi[:limit] (repeatable)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	m, err := load(*romPath)
	if err != nil {
		return err
	}
	if *loadPath != "" {
		f, err := os.Open(*loadPath)
		if err != nil {
			return err
		}
		err = m.LoadState(f)
		f.Close()
		if err != nil {
			return err
		}
	}
	presses, err := onepce.ParsePresses(*press)
	if err != nil {
		return err
	}
	m.Schedule(presses...)

	var ignorePCs []uint16
	for _, h := range strings.Split(*ignore, ",") {
		h = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(h, "0x"), "$"))
		if h == "" {
			continue
		}
		v, err := strconv.ParseUint(h, 16, 16)
		if err != nil {
			return fmt.Errorf("-ignore-pc %q: %w", h, err)
		}
		ignorePCs = append(ignorePCs, uint16(v))
	}

	type recorded struct {
		spec string
		w    *onepce.Watch
	}
	var recs []recorded
	out := os.Stdout
	if len(watches) > 0 {
		fmt.Fprintln(out, "kind\tspace\tsource\tframe\tscanline\thclock\tpc\topcode\taddr\tvalue\ta\tx\ty\ts\tp")
	}
	for _, spec := range watches {
		kind, space, lo, hi, limit, err := parseWatch(spec)
		if err != nil {
			return err
		}
		w := m.Watch(kind, space, lo, hi, func(e onepce.Event) {
			fmt.Fprintf(out, "%s\t%s\t%s\t%d\t%d\t%d\t%04X\t%02X\t%s\t%04X\t%02X\t%02X\t%02X\t%02X\t%02X\n",
				kindName(e.Kind), spaceName(e.Space), sourceName(e.Source), e.Frame, e.Scanline, e.HClock,
				e.PC, e.Opcode, e.Addr, e.Value, e.A, e.X, e.Y, e.S, e.P)
		})
		if limit > 0 {
			w.Limit(limit)
		}
		if len(ignorePCs) > 0 {
			w.IgnorePC(ignorePCs...)
		}
		recs = append(recs, recorded{spec, w})
	}

	var th *onepce.TraceHash
	if *traceHash {
		th = m.NewTraceHash()
	}
	if *wavPath != "" {
		m.SetAudioRate(*audioRate)
	}
	if *vgmWindow != "" {
		var start, stop uint64
		if _, err := fmt.Sscanf(*vgmWindow, "%d-%d", &start, &stop); err != nil || stop <= start {
			return fmt.Errorf("-vgm %q: want start-stop frames", *vgmWindow)
		}
		if *vgmPath == "" {
			return fmt.Errorf("-vgm needs -vgm-out")
		}
		m.RecordVGM(start, stop)
	}
	if *toFrame > 0 {
		m.RunToFrame(*toFrame)
	}

	for _, r := range recs {
		fmt.Fprintf(os.Stderr, "watch %s: count=%d skipped=%d ignored=%d\n", r.spec, r.w.Count(), r.w.Skipped(), r.w.Ignored())
	}
	if th != nil {
		sum, n := th.Sum()
		fmt.Fprintf(out, "trace_sha256\t%s\tinstructions\t%d\n", sum, n)
	}
	if *shot != "" {
		f, err := os.Create(*shot)
		if err != nil {
			return err
		}
		img := m.Framebuffer()
		err = png.Encode(f, img)
		f.Close()
		if err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "screenshot %s: %dx%d frame %d\n", *shot, img.Bounds().Dx(), img.Bounds().Dy(), m.Frame())
	}
	if *snapDir != "" {
		if err := writeSnapshot(m, *snapDir); err != nil {
			return err
		}
	}
	if *wavPath != "" {
		samples := m.DrainAudio()
		f, err := os.Create(*wavPath)
		if err != nil {
			return err
		}
		err = psg.WriteWAV(f, *audioRate, samples)
		f.Close()
		if err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "wav %s: %d stereo samples at %d Hz\n", *wavPath, len(samples)/2, *audioRate)
	}
	if *vgmPath != "" {
		data, done := m.VGM()
		if err := os.WriteFile(*vgmPath, data, 0o644); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "vgm %s: %d bytes, window closed=%v\n", *vgmPath, len(data), done)
	}
	if *savePath != "" {
		f, err := os.Create(*savePath)
		if err != nil {
			return err
		}
		err = m.SaveState(f)
		f.Close()
		if err != nil {
			return err
		}
	}
	fmt.Fprintf(os.Stderr, "onepce %s rom %s frame %d\n", onepce.Version, m.ROMHash(), m.Frame())
	return nil
}

func writeSnapshot(m *onepce.Machine, dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	snap := m.Snapshot()
	head := map[string]any{
		"version": snap.Version, "rom_sha256": snap.ROMHash, "frame": snap.Frame, "plan": snap.Plan,
		"cpu": snap.CPU, "mpr": snap.MPR[:], "vdc_regs": snap.VDCRegs, "hashes": snap.Hashes,
	}
	j, err := json.MarshalIndent(head, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "snapshot.json"), j, 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "ram.bin"), snap.RAM, 0o644); err != nil {
		return err
	}
	for name, words := range map[string][]uint16{"vram.bin": snap.VRAM, "sat.bin": snap.SAT, "palette.bin": snap.VCE} {
		b := make([]byte, 2*len(words))
		for i, v := range words {
			b[2*i], b[2*i+1] = byte(v), byte(v>>8)
		}
		if err := os.WriteFile(filepath.Join(dir, name), b, 0o644); err != nil {
			return err
		}
	}
	return nil
}

func parseWatch(spec string) (onepce.Kind, onepce.Space, uint32, uint32, int, error) {
	parts := strings.Split(spec, ":")
	if len(parts) < 3 || len(parts) > 4 {
		return 0, 0, 0, 0, 0, fmt.Errorf("-watch %q: want kind:space:lo-hi[:limit]", spec)
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
		return 0, 0, 0, 0, 0, fmt.Errorf("-watch %q: kind must be read|write|exec", spec)
	}
	var space onepce.Space
	switch parts[1] {
	case "cpu":
		space = onepce.CPU
	case "vram":
		space = onepce.VRAM
	default:
		return 0, 0, 0, 0, 0, fmt.Errorf("-watch %q: space must be cpu|vram", spec)
	}
	rng := strings.SplitN(parts[2], "-", 2)
	lo, err := strconv.ParseUint(strings.TrimPrefix(rng[0], "$"), 16, 32)
	if err != nil {
		return 0, 0, 0, 0, 0, fmt.Errorf("-watch %q: lo: %w", spec, err)
	}
	hi := lo
	if len(rng) == 2 {
		hi, err = strconv.ParseUint(strings.TrimPrefix(rng[1], "$"), 16, 32)
		if err != nil {
			return 0, 0, 0, 0, 0, fmt.Errorf("-watch %q: hi: %w", spec, err)
		}
	}
	limit := 0
	if len(parts) == 4 {
		limit, err = strconv.Atoi(parts[3])
		if err != nil {
			return 0, 0, 0, 0, 0, fmt.Errorf("-watch %q: limit: %w", spec, err)
		}
	}
	return kind, space, uint32(lo), uint32(hi), limit, nil
}

func kindName(k onepce.Kind) string {
	return [...]string{"read", "write", "exec"}[k]
}

func spaceName(s onepce.Space) string {
	return [...]string{"cpu", "vram"}[s]
}

func sourceName(s onepce.Source) string {
	return [...]string{"cpu", "dma", "satb"}[s]
}

func rpcCommand(args []string) error {
	fs := flag.NewFlagSet("rpc", flag.ContinueOnError)
	romPath := fs.String("rom", "", "HuCard image (.pce)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	m, err := load(*romPath)
	if err != nil {
		return err
	}
	return rpc.New(m).Serve(os.Stdin, os.Stdout)
}

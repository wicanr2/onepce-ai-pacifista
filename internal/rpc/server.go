// Package rpc is the JSON-RPC 2.0 face of the emulator over a line stream
// (stdin/stdout). It exposes the root package's methods one to one; it does
// not add semantics of its own. Spec: docs/spec/cli-rpc.md.
package rpc

import (
	"bufio"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image/png"
	"io"
	"os"

	"github.com/wicanr2/onepce-ai-pacifista"
)

const maxQueue = 100000

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type watchEntry struct {
	w       *onepce.Watch
	queue   []onepce.Event
	dropped int
}

// Server serves one machine.
type Server struct {
	m       *onepce.Machine
	watches map[int]*watchEntry
	nextID  int
	trace   *onepce.TraceHash
}

// New wraps a loaded machine.
func New(m *onepce.Machine) *Server {
	return &Server{m: m, watches: map[int]*watchEntry{}}
}

// Serve reads one request per line until EOF.
func (s *Server) Serve(r io.Reader, w io.Writer) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	enc := json.NewEncoder(w)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var req request
		resp := response{JSONRPC: "2.0"}
		if err := json.Unmarshal(line, &req); err != nil {
			resp.Error = &rpcError{Code: -32700, Message: err.Error()}
		} else {
			resp.ID = req.ID
			result, err := s.dispatch(req.Method, req.Params)
			if err != nil {
				resp.Error = &rpcError{Code: err.code, Message: err.Error()}
			} else {
				resp.Result = result
			}
		}
		if err := enc.Encode(resp); err != nil {
			return err
		}
	}
	return sc.Err()
}

type codedError struct {
	code int
	err  error
}

func (e *codedError) Error() string { return e.err.Error() }

func badParams(err error) *codedError { return &codedError{code: -32602, err: err} }
func failed(err error) *codedError    { return &codedError{code: -32000, err: err} }

func (s *Server) frame() uint64 { return s.m.Frame() }

// Handle runs one method; exported for tests and for the CLI.
func (s *Server) Handle(method string, params json.RawMessage) (any, error) {
	r, err := s.dispatch(method, params)
	if err != nil {
		return nil, err
	}
	return r, nil
}

func (s *Server) dispatch(method string, params json.RawMessage) (any, *codedError) {
	decode := func(v any) *codedError {
		if len(params) == 0 {
			return nil
		}
		if err := json.Unmarshal(params, v); err != nil {
			return badParams(err)
		}
		return nil
	}
	switch method {
	case "info":
		return map[string]any{"version": onepce.Version, "rom_sha256": s.m.ROMHash(), "frame": s.frame()}, nil

	case "schedule":
		var p struct {
			Presses []struct {
				Frame  uint64 `json:"frame"`
				Button string `json:"button"`
				Span   int    `json:"span"`
			} `json:"presses"`
		}
		if e := decode(&p); e != nil {
			return nil, e
		}
		for _, pr := range p.Presses {
			b, err := onepce.ButtonByName(pr.Button)
			if err != nil || pr.Span <= 0 {
				return nil, badParams(fmt.Errorf("press %+v: %v", pr, err))
			}
			s.m.Schedule(onepce.Press{Frame: pr.Frame, Button: b, Span: pr.Span})
		}
		return map[string]any{"count": len(p.Presses), "frame": s.frame()}, nil

	case "run_frames":
		var p struct {
			N int `json:"n"`
		}
		if e := decode(&p); e != nil {
			return nil, e
		}
		s.m.RunFrames(p.N)
		return map[string]any{"frame": s.frame()}, nil

	case "run_to_frame":
		var p struct {
			Frame uint64 `json:"frame"`
		}
		if e := decode(&p); e != nil {
			return nil, e
		}
		s.m.RunToFrame(p.Frame)
		return map[string]any{"frame": s.frame()}, nil

	case "step":
		var p struct {
			N int `json:"n"`
		}
		if e := decode(&p); e != nil {
			return nil, e
		}
		if p.N <= 0 {
			p.N = 1
		}
		for i := 0; i < p.N; i++ {
			s.m.Step()
		}
		r := s.m.Registers()
		return map[string]any{"frame": s.frame(), "pc": r.PC, "cycles": r.Cycles}, nil

	case "registers":
		r := s.m.Registers()
		mpr := s.m.MPR()
		return map[string]any{"frame": s.frame(), "pc": r.PC, "opcode": r.Opcode, "a": r.A, "x": r.X, "y": r.Y,
			"s": r.S, "p": r.P, "cycles": r.Cycles, "mpr": mpr[:]}, nil

	case "peek":
		var p struct {
			Addr uint16 `json:"addr"`
			Len  int    `json:"len"`
		}
		if e := decode(&p); e != nil {
			return nil, e
		}
		if p.Len <= 0 {
			p.Len = 1
		}
		buf := make([]byte, p.Len)
		for i := range buf {
			buf[i] = s.m.Peek(p.Addr + uint16(i))
		}
		return map[string]any{"frame": s.frame(), "bytes": hex.EncodeToString(buf)}, nil

	case "poke":
		var p struct {
			Addr  uint16 `json:"addr"`
			Value uint8  `json:"value"`
		}
		if e := decode(&p); e != nil {
			return nil, e
		}
		return map[string]any{"frame": s.frame(), "written": s.m.Poke(p.Addr, p.Value)}, nil

	case "hold":
		var p struct {
			Addr  uint16 `json:"addr"`
			Value uint8  `json:"value"`
		}
		if e := decode(&p); e != nil {
			return nil, e
		}
		return map[string]any{"frame": s.frame(), "held": s.m.Hold(p.Addr, p.Value)}, nil

	case "unhold":
		var p struct {
			Addr uint16 `json:"addr"`
		}
		if e := decode(&p); e != nil {
			return nil, e
		}
		s.m.Unhold(p.Addr)
		return map[string]any{"frame": s.frame()}, nil

	case "resolve":
		var p struct {
			Addr uint16 `json:"addr"`
		}
		if e := decode(&p); e != nil {
			return nil, e
		}
		a := s.m.Resolve(p.Addr)
		return map[string]any{"frame": s.frame(), "logical": a.Logical, "physical": a.Physical, "file": a.File,
			"mpr": a.MPR[:], "text": a.String()}, nil

	case "snapshot":
		var p struct {
			Sections []string `json:"sections"`
		}
		if e := decode(&p); e != nil {
			return nil, e
		}
		var secs []onepce.Section
		for _, name := range p.Sections {
			secs = append(secs, onepce.Section(name))
		}
		snap := s.m.Snapshot(secs...)
		sections := map[string]string{}
		if snap.RAM != nil {
			sections["RAM"] = base64.StdEncoding.EncodeToString(snap.RAM)
		}
		if snap.VRAM != nil {
			sections["VRAM"] = base64.StdEncoding.EncodeToString(wordsToBytes(snap.VRAM))
		}
		if snap.SAT != nil {
			sections["SAT"] = base64.StdEncoding.EncodeToString(wordsToBytes(snap.SAT))
		}
		if snap.VCE != nil {
			sections["VCE"] = base64.StdEncoding.EncodeToString(wordsToBytes(snap.VCE))
		}
		return map[string]any{"frame": snap.Frame, "rom_sha256": snap.ROMHash, "version": snap.Version,
			"hashes": snap.Hashes, "sections": sections, "cpu": snap.CPU, "mpr": snap.MPR[:],
			"vdc_regs": snap.VDCRegs}, nil

	case "watch":
		var p struct {
			Kind     string   `json:"kind"`
			Space    string   `json:"space"`
			Lo       uint32   `json:"lo"`
			Hi       uint32   `json:"hi"`
			Limit    int      `json:"limit"`
			IgnorePC []uint16 `json:"ignore_pc"`
			Bank     *int     `json:"bank"`
			FileLo   *int64   `json:"file_lo"`
			FileHi   *int64   `json:"file_hi"`
		}
		if e := decode(&p); e != nil {
			return nil, e
		}
		kind, space, err := parseKindSpace(p.Kind, p.Space)
		if err != nil {
			return nil, badParams(err)
		}
		s.nextID++
		id := s.nextID
		entry := &watchEntry{}
		entry.w = s.m.Watch(kind, space, p.Lo, p.Hi, func(e onepce.Event) {
			if len(entry.queue) >= maxQueue {
				entry.dropped++
				return
			}
			entry.queue = append(entry.queue, e)
		})
		if p.Limit > 0 {
			entry.w.Limit(p.Limit)
		}
		if len(p.IgnorePC) > 0 {
			entry.w.IgnorePC(p.IgnorePC...)
		}
		switch {
		case p.Bank != nil:
			entry.w.InBank(*p.Bank)
		case p.FileLo != nil && p.FileHi != nil:
			entry.w.InFile(*p.FileLo, *p.FileHi)
		}
		s.watches[id] = entry
		return map[string]any{"id": id, "frame": s.frame()}, nil

	case "callers":
		var p struct {
			Max int `json:"max"`
		}
		if e := decode(&p); e != nil {
			return nil, e
		}
		if p.Max <= 0 {
			p.Max = 16
		}
		callers := s.m.Callers(p.Max)
		out := make([]map[string]any, 0, len(callers))
		for _, c := range callers {
			out = append(out, map[string]any{"stack": c.Stack, "return": c.Return, "kind": c.Kind,
				"call": map[string]any{"logical": c.Call.Logical, "physical": c.Call.Physical, "file": c.Call.File}})
		}
		return map[string]any{"frame": s.frame(), "callers": out}, nil

	case "events":
		var p struct {
			ID  int `json:"id"`
			Max int `json:"max"`
		}
		if e := decode(&p); e != nil {
			return nil, e
		}
		entry, ok := s.watches[p.ID]
		if !ok {
			return nil, badParams(fmt.Errorf("no watch %d", p.ID))
		}
		if p.Max <= 0 || p.Max > len(entry.queue) {
			p.Max = len(entry.queue)
		}
		out := entry.queue[:p.Max]
		entry.queue = append([]onepce.Event(nil), entry.queue[p.Max:]...)
		return map[string]any{"frame": s.frame(), "events": out, "count": entry.w.Count(),
			"skipped": entry.w.Skipped() + entry.dropped, "ignored": entry.w.Ignored(), "remaining": len(entry.queue)}, nil

	case "unwatch":
		var p struct {
			ID int `json:"id"`
		}
		if e := decode(&p); e != nil {
			return nil, e
		}
		entry, ok := s.watches[p.ID]
		if !ok {
			return nil, badParams(fmt.Errorf("no watch %d", p.ID))
		}
		entry.w.Remove()
		delete(s.watches, p.ID)
		return map[string]any{"frame": s.frame()}, nil

	case "screenshot":
		var p struct {
			Path string `json:"path"`
		}
		if e := decode(&p); e != nil {
			return nil, e
		}
		img := s.m.Framebuffer()
		f, err := os.Create(p.Path)
		if err != nil {
			return nil, failed(err)
		}
		defer f.Close()
		if err := png.Encode(f, img); err != nil {
			return nil, failed(err)
		}
		return map[string]any{"frame": s.frame(), "width": img.Bounds().Dx(), "height": img.Bounds().Dy()}, nil

	case "save_state":
		var p struct {
			Path string `json:"path"`
		}
		if e := decode(&p); e != nil {
			return nil, e
		}
		f, err := os.Create(p.Path)
		if err != nil {
			return nil, failed(err)
		}
		defer f.Close()
		if err := s.m.SaveState(f); err != nil {
			return nil, failed(err)
		}
		return map[string]any{"frame": s.frame()}, nil

	case "load_state":
		var p struct {
			Path string `json:"path"`
		}
		if e := decode(&p); e != nil {
			return nil, e
		}
		f, err := os.Open(p.Path)
		if err != nil {
			return nil, failed(err)
		}
		defer f.Close()
		if err := s.m.LoadState(f); err != nil {
			return nil, failed(err)
		}
		return map[string]any{"frame": s.frame()}, nil

	case "trace_hash":
		var p struct {
			Action string `json:"action"`
		}
		if e := decode(&p); e != nil {
			return nil, e
		}
		switch p.Action {
		case "start":
			if s.trace != nil {
				s.trace.Detach()
			}
			s.trace = s.m.NewTraceHash()
			return map[string]any{"frame": s.frame()}, nil
		case "stop", "":
			if s.trace == nil {
				return nil, badParams(fmt.Errorf("no trace running"))
			}
			sum, n := s.trace.Sum()
			s.trace.Detach()
			s.trace = nil
			return map[string]any{"frame": s.frame(), "sha256": sum, "instructions": n}, nil
		}
		return nil, badParams(fmt.Errorf("action must be start or stop"))
	}
	return nil, &codedError{code: -32601, err: fmt.Errorf("unknown method %q", method)}
}

func parseKindSpace(kind, space string) (onepce.Kind, onepce.Space, error) {
	var k onepce.Kind
	switch kind {
	case "read":
		k = onepce.Read
	case "write":
		k = onepce.Write
	case "exec":
		k = onepce.Exec
	default:
		return 0, 0, fmt.Errorf("kind must be read|write|exec")
	}
	var sp onepce.Space
	switch space {
	case "cpu", "":
		sp = onepce.CPU
	case "vram":
		sp = onepce.VRAM
	default:
		return 0, 0, fmt.Errorf("space must be cpu|vram")
	}
	return k, sp, nil
}

func wordsToBytes(w []uint16) []byte {
	b := make([]byte, 2*len(w))
	for i, v := range w {
		b[2*i] = byte(v)
		b[2*i+1] = byte(v >> 8)
	}
	return b
}

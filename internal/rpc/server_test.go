package rpc

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/wicanr2/onepce-ai-remake"
)

// The same probe as the root package tests: map RAM/IO, write $42 to zero
// page, write $BEEF to VRAM[$1234], loop.
var probe = []uint8{
	0xA9, 0xF8, 0x53, 0x02, 0xA9, 0xFF, 0x53, 0x01, 0xA9, 0x42, 0x85, 0x10,
	0x03, 0x00, 0x13, 0x34, 0x23, 0x12, 0x03, 0x02, 0x13, 0xEF, 0x23, 0xBE, 0x80, 0xFE,
}

func newServer(t *testing.T) *Server {
	t.Helper()
	rom := make([]byte, 0x2000)
	copy(rom, probe)
	rom[0x1FFE], rom[0x1FFF] = 0x00, 0xE0
	m, err := onepce.Load(rom)
	if err != nil {
		t.Fatal(err)
	}
	return New(m)
}

func call(t *testing.T, s *Server, method, params string) map[string]any {
	t.Helper()
	var in bytes.Buffer
	fmt.Fprintf(&in, `{"jsonrpc":"2.0","id":1,"method":%q,"params":%s}`+"\n", method, params)
	var out bytes.Buffer
	if err := s.Serve(&in, &out); err != nil {
		t.Fatal(err)
	}
	var resp map[string]any
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		t.Fatalf("%s: %v: %s", method, err, out.String())
	}
	if e, ok := resp["error"]; ok && e != nil {
		t.Fatalf("%s: error %v", method, e)
	}
	r, _ := resp["result"].(map[string]any)
	return r
}

func TestRoundTripOverTheLineProtocol(t *testing.T) {
	s := newServer(t)
	info := call(t, s, "info", `{}`)
	if info["version"] != onepce.Version {
		t.Fatalf("info %v", info)
	}
	w := call(t, s, "watch", `{"kind":"write","space":"vram","lo":4660,"hi":4660}`)
	id := int(w["id"].(float64))
	call(t, s, "step", `{"n":12}`)
	ev := call(t, s, "events", fmt.Sprintf(`{"id":%d}`, id))
	events := ev["events"].([]any)
	if len(events) != 1 || ev["count"].(float64) != 1 {
		t.Fatalf("events %v", ev)
	}
	first := events[0].(map[string]any)
	if first["Value"].(float64) != 0xBEEF || first["PC"].(float64) != 0xE016 {
		t.Fatalf("event %v", first)
	}
	peek := call(t, s, "peek", `{"addr":8208,"len":1}`) // $2010
	if peek["bytes"] != "42" {
		t.Fatalf("peek %v", peek)
	}
	snap := call(t, s, "snapshot", `{"sections":["RAM","VRAM"]}`)
	hashes := snap["hashes"].(map[string]any)
	if _, ok := hashes["RAM"]; !ok {
		t.Fatalf("snapshot %v", snap)
	}
	res := call(t, s, "resolve", `{"addr":8208}`)
	if !strings.HasPrefix(res["text"].(string), "L:$2010 P:$1F0010 F:unknown") {
		t.Fatalf("resolve %v", res)
	}
	call(t, s, "unwatch", fmt.Sprintf(`{"id":%d}`, id))
	call(t, s, "trace_hash", `{"action":"start"}`)
	call(t, s, "step", `{"n":5}`)
	th := call(t, s, "trace_hash", `{"action":"stop"}`)
	if th["instructions"].(float64) != 5 {
		t.Fatalf("trace %v", th)
	}
}

func TestErrorsAreReportedNotSwallowed(t *testing.T) {
	s := newServer(t)
	var in bytes.Buffer
	in.WriteString(`{"jsonrpc":"2.0","id":7,"method":"watch","params":{"kind":"bogus"}}` + "\n")
	in.WriteString(`{"jsonrpc":"2.0","id":8,"method":"nope"}` + "\n")
	var out bytes.Buffer
	if err := s.Serve(&in, &out); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 || !strings.Contains(lines[0], `"code":-32602`) || !strings.Contains(lines[1], `"code":-32601`) {
		t.Fatalf("responses: %v", lines)
	}
}

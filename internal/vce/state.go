package vce

// State is the serialisable colour encoder (docs/spec/state.md S4).
type State struct {
	Palette [0x200]uint16
	Addr    uint16
	Divider int
	Lines   int
	Gray    bool
}

// Save copies the state out.
func (c *VCE) Save() State {
	return State{Palette: c.palette, Addr: c.addr, Divider: c.divider, Lines: c.lines, Gray: c.grayscal}
}

// Restore loads the state.
func (c *VCE) Restore(s State) {
	c.palette, c.addr, c.divider, c.lines, c.grayscal = s.Palette, s.Addr, s.Divider, s.Lines, s.Gray
}

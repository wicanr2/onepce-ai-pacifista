package huc6280

// State is the serialisable register file (docs/spec/state.md S4/S6).
type State struct {
	A, X, Y, S, P uint8
	PC, InstPC    uint16
	Cycles        uint64
	IRQSample     uint8
}

// Save copies the registers out.
func (c *CPU) Save() State {
	return State{A: c.A, X: c.X, Y: c.Y, S: c.S, P: c.P, PC: c.PC, InstPC: c.InstPC,
		Cycles: c.Cycles, IRQSample: c.irqSample}
}

// Restore loads the registers; the bus binding is untouched.
func (c *CPU) Restore(s State) {
	c.A, c.X, c.Y, c.S, c.P = s.A, s.X, s.Y, s.S, s.P
	c.PC, c.InstPC = s.PC, s.InstPC
	c.Cycles = s.Cycles
	c.irqSample = s.IRQSample
}

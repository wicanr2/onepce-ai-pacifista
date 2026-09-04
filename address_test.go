package onepce

import "testing"

// The paging state Nectaris runs its tactical loop under (docs/pc-engine of
// nectaris-cht, P-071): MPR7 is $00 there, which is what makes the
// reset-vector page land at the start of the ROM file.
var tacticalMPR = MPR{0xFF, 0xF8, 0x13, 0x14, 0x01, 0x02, 0x03, 0x00}

func TestPhysicalCombinesBankAndPageOffset(t *testing.T) {
	cases := []struct {
		logical uint16
		want    uint32
	}{
		{0x0000, 0x1FE000}, // page 0, MPR0=$FF → I/O bank
		{0x1FFF, 0x1FFFFF}, // top of page 0
		{0x2000, 0x1F0000}, // page 1, MPR1=$F8 → work RAM
		{0x6151, 0x028151}, // page 3, MPR3=$14 → $14<<13 = $28000
		{0xE000, 0x000000}, // page 7, MPR7=$00
		{0xFFFF, 0x001FFF},
	}
	for _, c := range cases {
		if got := tacticalMPR.Physical(c.logical); got != c.want {
			t.Errorf("Physical($%04X) = $%06X, want $%06X", c.logical, got, c.want)
		}
	}
}

func TestBankPicksTheRegisterForThePage(t *testing.T) {
	if got := tacticalMPR.Bank(0x6151); got != 0x14 {
		t.Fatalf("Bank($6151) = $%02X, want $14", got)
	}
}

func TestAddressStringIsTheFixedFormat(t *testing.T) {
	a := Resolve(0x6151, tacticalMPR)
	if a.File != FileUnknown {
		t.Fatalf("Resolve must not invent a file offset, got %d", a.File)
	}
	if got, want := a.String(), "L:$6151 P:$28151 F:unknown MPR=[FF F8 13 14 01 02 03 00]"; got != want {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
	a.File = 0x0C151
	if got, want := a.String(), "L:$6151 P:$28151 F:0x0C151 MPR=[FF F8 13 14 01 02 03 00]"; got != want {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
}

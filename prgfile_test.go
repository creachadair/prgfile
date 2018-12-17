// Copyright (C) 2018 Michael J. Fromberger. All Rights Reserved.

package prgfile

import (
	"fmt"
	"io"
	"strings"
	"testing"
)

func run(input string) (string, error) {
	r, err := New(strings.NewReader(input))
	if err != nil {
		return "", fmt.Errorf("New(%q): %v", input, err)
	}

	var got strings.Builder
	fmt.Fprintf(&got, "@%04x\n", r.Origin())
	for {
		next, err := r.Line()
		if err == io.EOF {
			break
		} else if err != nil {
			return "", err
		}
		fmt.Fprintf(&got, "%04x %d ", next.Addr, next.N)
		for i, insn := range next.Insn {
			if i > 0 {
				got.WriteByte(':')
			}
			got.WriteString(insn)
		}
		got.WriteByte('\n')
	}
	return got.String(), nil
}

func TestReader(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		// Empty program at origin 0xc030
		{"\x30\xc0\x00\x00", "@c030\n"},

		// One line with no instructions at 0x0000.
		{"\x00\x00\x04\x00\x00\x00\x00\x00\x00", "@0000\n0000 0 \n"},

		// One line with a single END instruction.
		{"\x01\x00\x03\x00\x0a\x00\x80\x00\x00\x00", `@0001
0001 10 END
`},

		// Example based on https://www.c64-wiki.com/wiki/BASIC_token.
		{"\x01\x08\x15\x08\x64\x00\x99 \"HELLO WORLD\"\x00\x1c\x08\x6e\x00\x89100\x00\x00\x00",
			`@0801
0801 100 PRINT "HELLO WORLD"
0815 110 GOTO 100
`},
	}
	for _, test := range tests {
		got, err := run(test.input)
		if err != nil {
			t.Errorf("Reading %q: unexpected error: %v", test.input, err)
		} else if got != test.want {
			t.Errorf("Reading %q:\n got: %#q\nwant: %#q", test.input, got, test.want)
		}
	}
}

func TestReaderErrors(t *testing.T) {
	tests := []struct {
		input string
	}{
		{""},                                // no origin
		{"\x01\x01"},                        // missing address
		{"\x01\x00\x03\x00\x80"},            // missing end of instruction marker
		{"\x01\x02\x03\x04\x00\x00X=5\x00"}, // missing end-of-file marker
	}
	for _, test := range tests {
		got, err := run(test.input)
		if err == nil {
			t.Errorf("Reading %q: got %#q, wanted error", test.input, got)
		} else {
			t.Logf("Reading %q: got expected error: %v", test.input, err)
		}
	}
}

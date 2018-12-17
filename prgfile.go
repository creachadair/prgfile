// Copyright (C) 2018 Michael J. Fromberger. All Rights Reserved.

// Package prgfile reads programs from Commodore BASIC tokenized (PRG) files.
//
// Usage:
//    pr, err := prgfile.New(r)
//    ...
//    for {
//       line, err := pr.Line()
//       if err == io.EOF {
//          break
//       } else if err != nil {
//          log.Fatal("Failed: %v", err)
//       }
//       doSomethingWith(line)
//    }
//
package prgfile

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

/*
  File grammar:

    file  = org [lines]
    lines = line [lines]
    line  = eof
          | addr lnum 0*insn eol line
    org   = WORD
    eof   = \x00 \x00
    addr  = WORD
    lnum  = WORD
    insn  = 1*BYTE
    eol   = \x00
    WORD  = [2]BYTE ;; LSB first

  http://fileformats.archiveteam.org/wiki/Commodore_BASIC_tokenized_file
*/

const tokenBase = 128 // smallest byte value for a token

var spelling = []string{
	// offset = code - tokenBase
	"END", "FOR", "NEXT", "DATA", "INPUT#", "INPUT", "DIM", "READ", "LET",
	"GOTO", "RUN", "IF", "RESTORE", "GOSUB", "RETURN", "REM", "STOP", "ON",
	"WAIT", "LOAD", "SAVE", "VERIFY", "DEF", "POKE", "PRINT#", "PRINT", "CONT",
	"LIST", "CLR", "CMD", "SYS", "OPEN", "CLOSE", "GET", "NEW", "TAB(", "TO",
	"FN", "SPC(", "THEN", "NOT", "STEP", "+", "-", "*", "/", "^", "AND", "OR",
	">", "=", "<", "SGN", "INT", "ABS", "USR", "FRE", "POS", "SQR", "RND",
	"LOG", "EXP", "COS", "SIN", "TAN", "ATN", "PEEK", "LEN", "STR$", "VAL",
	"ASC", "CHR$", "LEFT$", "RIGHT$", "MID$", "GO",
}

func isToken(ch byte) (string, bool) {
	v := int(ch) - tokenBase
	if v >= 0 && v < len(spelling) {
		return spelling[v], true
	}
	return "", false
}

// A Reader parses a tokenized program and returns lines containing the decoded
// instructions.
type Reader struct {
	org      uint16 // origin address from the stream header
	nextAddr uint16 // base address of next line (0 at start)

	buf *bufio.Reader
	pos int
}

// word returns the value of the next 2 bytes of input as a little-endian
// unsigned value, and advances the offset.
func (r *Reader) word() (uint16, error) {
	var word [2]byte
	_, err := io.ReadFull(r.buf, word[:])
	if err != nil {
		return 0, err
	}
	r.pos += 2
	return (uint16(word[1]) << 8) | uint16(word[0]), nil
}

// byte returns the next byte of input, and advances the offset.
func (r *Reader) byte() (byte, error) {
	b, err := r.buf.ReadByte()
	if err == nil {
		r.pos++
	}
	return b, err
}

// fail decorates an error message with location information.
func (r *Reader) fail(msg string, args ...interface{}) error {
	return fmt.Errorf("offset %d: %s", r.pos, fmt.Sprintf(msg, args...))
}

// New constructs a *Reader that consumes input from r, which is expected to be
// positioned at the origin mark beginning a PRG file.
func New(r io.Reader) (*Reader, error) {
	rd := &Reader{buf: bufio.NewReader(r)}
	org, err := rd.word()
	if err != nil {
		return nil, rd.fail("reading origin: %v", err)
	}
	rd.org = org
	rd.nextAddr = org
	return rd, nil
}

// A Line represents a single program line containing instructions.
type Line struct {
	N    uint16   // line number
	Addr uint16   // memory address of first instruction
	Insn []string // instructions on this line
}

// An insn represents a single BASIC instruction.
type insn []string

// isWord reports whether ch should be considered part of a "word", which
// notionally is something written with letters and digits, but for our
// purposes includes quotation marks so string literals are units.
func isWord(ch byte) bool {
	return ch == '"' || // honorary
		ch >= 'A' && ch <= 'Z' ||
		ch >= 'a' && ch <= 'z' ||
		ch >= '0' && ch <= '9'
}

func endsWord(s string) bool   { return s != "" && isWord(s[len(s)-1]) }
func startsWord(s string) bool { return s != "" && isWord(s[0]) }

// String renders the instruction with suitable whitespace inserted or removed
// between consecutive tokens.
func (in insn) String() string {
	var str strings.Builder
	ew := false
	for _, w := range in {
		if ew && startsWord(w) {
			str.WriteByte(' ')
		}
		str.WriteString(w)
		ew = endsWord(w)
	}
	return str.String()
}

// Origin returns the origin address for the input.
func (r *Reader) Origin() uint16 { return r.org }

// Pos returns the current byte offset in the input.
func (r *Reader) Pos() int { return r.pos }

// Line parses and returns the next line from the input.
// It returns nil, io.EOF when the end of instruction marker is reached.
func (r *Reader) Line() (*Line, error) {
	addr := r.nextAddr

	// Read the next line address from the line prefix.
	next, err := r.word()
	if err != nil {
		return nil, r.fail("reading next address: %v", err)
	}
	r.nextAddr = next
	if next == 0 {
		return nil, io.EOF
	}

	// Read the current line number.
	lnum, err := r.word()
	if err != nil {
		return nil, r.fail("reading line number: %v", err)
	}

	// Collect instructions.
	var insns []string      // instructions on current line
	var words insn          // words in current instruction
	var cur strings.Builder // current word
	quoted := false         // currently inside quotes

	// Push the current word onto the instruction.
	push := func() {
		if cur.Len() != 0 {
			words = append(words, cur.String())
			cur.Reset()
		}
	}

	// Push the current instruction onto the line.
	emit := func() {
		push()
		if len(words) != 0 {
			insns = append(insns, words.String())
			words = nil
		}
	}

	for {
		ch, err := r.byte()
		if err != nil {
			return nil, r.fail("reading instruction: %v", err)
		} else if ch == 0 {
			emit()
			break // end of line
		}

		// An unquoted token is expanded to its spelling. This delimits any
		// previous in-progress word.
		if s, ok := isToken(ch); ok && !quoted {
			push()
			words = append(words, s)
			continue
		}

		// Double quotes toggle string literals, inside which tokens are not
		// expanded (though in principle they should not occur there anyway).
		if ch == '"' {
			quoted = !quoted
		} else if ch == ':' && !quoted {
			// An un-quoted colon is treated as its own token, even though it does
			// not appear in the token grammar. This allows instructions to be
			// distinguished later.
			emit()
			continue
		}
		cur.WriteByte(ch)
	}

	return &Line{
		N:    lnum,
		Addr: addr,
		Insn: insns,
	}, nil
}

// Package hocuspocus implements the wire protocol the collaborative editor's client speaks.
//
// It is the Hocuspocus framing rather than plain y-websocket: every frame carries the document's name in front of it, so one socket can serve several documents, and there are message types beyond the two y-protocols defines — authentication, stateless signals, a save acknowledgement and a heartbeat.
//
// The encoding underneath is lib0's: unsigned LEB128 for a number, a length-prefixed run of bytes for everything else.
package hocuspocus

import (
	"errors"
	"fmt"
	"unicode/utf8"
)

// ErrTruncated is returned when a frame ends in the middle of a value.
var ErrTruncated = errors.New("hocuspocus: message ended early")

// Encoder builds a frame.
type Encoder struct {
	buffer []byte
}

// Bytes is the frame built so far.
func (e *Encoder) Bytes() []byte { return e.buffer }

// Len is how many bytes have been written, which is what the server compares against to decide whether a reply carries anything worth sending.
func (e *Encoder) Len() int { return len(e.buffer) }

// WriteVarUint writes an unsigned LEB128 number.
func (e *Encoder) WriteVarUint(value uint64) {
	for value > 0x7f {
		e.buffer = append(e.buffer, byte(0x80|(value&0x7f)))
		value >>= 7
	}
	e.buffer = append(e.buffer, byte(value&0x7f))
}

// WriteVarUint8Array writes a run of bytes behind its length.
func (e *Encoder) WriteVarUint8Array(payload []byte) {
	e.WriteVarUint(uint64(len(payload)))
	e.buffer = append(e.buffer, payload...)
}

// WriteVarString writes a string as its UTF-8 bytes behind their length. The length counts bytes rather than characters, which is what makes a name with an emoji in it round-trip.
func (e *Encoder) WriteVarString(value string) {
	e.WriteVarUint8Array([]byte(value))
}

// Decoder reads a frame.
type Decoder struct {
	buffer []byte
	offset int
}

// NewDecoder reads the given frame.
func NewDecoder(frame []byte) *Decoder { return &Decoder{buffer: frame} }

// Remaining is the part of the frame that has not been read, which several message types hand over whole.
func (d *Decoder) Remaining() []byte { return d.buffer[d.offset:] }

// HasMore reports whether anything is left to read.
func (d *Decoder) HasMore() bool { return d.offset < len(d.buffer) }

// ReadVarUint reads an unsigned LEB128 number.
func (d *Decoder) ReadVarUint() (uint64, error) {
	var value uint64
	var shift uint
	for {
		if d.offset >= len(d.buffer) {
			return 0, ErrTruncated
		}
		current := d.buffer[d.offset]
		d.offset++
		value |= uint64(current&0x7f) << shift
		if current&0x80 == 0 {
			return value, nil
		}
		shift += 7
		if shift > 63 {
			return 0, fmt.Errorf("hocuspocus: number does not fit in 64 bits")
		}
	}
}

// ReadVarUint8Array reads a length-prefixed run of bytes. The result points into the frame rather than copying it, so a caller that keeps it must keep the frame too.
func (d *Decoder) ReadVarUint8Array() ([]byte, error) {
	length, err := d.ReadVarUint()
	if err != nil {
		return nil, err
	}
	if uint64(len(d.buffer)-d.offset) < length {
		return nil, ErrTruncated
	}
	start := d.offset
	d.offset += int(length)
	return d.buffer[start:d.offset], nil
}

// ReadVarString reads a length-prefixed string. Bytes that are not valid UTF-8 are rejected rather than replaced, because a name that does not round-trip would address a different document than the one the client meant.
func (d *Decoder) ReadVarString() (string, error) {
	payload, err := d.ReadVarUint8Array()
	if err != nil {
		return "", err
	}
	if !utf8.Valid(payload) {
		return "", fmt.Errorf("hocuspocus: string is not valid utf-8")
	}
	return string(payload), nil
}

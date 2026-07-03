// Package jsonl implements sink.Sink as JSON Lines: one JSON object per
// classified record, the pipe-friendly lingua franca of log tooling.
//
// The wire shape per line is {"id", "kind", "data", "fields", "meta",
// "category", "confidence", "stage", "reasons"}, where kind, fields, meta,
// stage, and reasons appear only when present — and kind only for structured
// payloads, so a plain text record serializes exactly as it always has.
package jsonl

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"unicode/utf8"

	"github.com/ClassMesh/classmesh/shared/pkg/domain"
	"github.com/ClassMesh/classmesh/shared/pkg/sink"
)

// Sink writes one JSON object per record to an io.Writer. Output is
// buffered; Close flushes. The underlying writer is not closed — the caller
// owns it (it is usually stdout). Not safe for concurrent Write: the engine
// drives a sink from one goroutine, and the scratch buffers rely on that.
type Sink struct {
	w    io.Writer
	buf  []byte
	keys []string

	fieldsBuf bytes.Buffer
	fieldsEnc *json.Encoder
}

var _ sink.Sink = (*Sink)(nil)

// writeBufferSize is large enough that high-volume JSONL output flushes to the
// underlying writer in few syscalls rather than line-sized writes.
const writeBufferSize = 64 << 10

// New returns a Sink writing JSON Lines to w.
func New(w io.Writer) *Sink {
	s := &Sink{w: w, buf: make([]byte, 0, writeBufferSize)}
	s.fieldsEnc = json.NewEncoder(&s.fieldsBuf)
	s.fieldsEnc.SetEscapeHTML(false)
	return s
}

// Write implements sink.Sink, appending one record to the output buffer and
// flushing it once it fills.
func (s *Sink) Write(ctx context.Context, r domain.Record, c domain.Classification) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	n := len(s.buf)
	b, err := s.appendRecord(s.buf, r, c)
	if err != nil {
		s.buf = s.buf[:n]
		return fmt.Errorf("jsonl: encode %s: %w", r.ID, err)
	}
	s.buf = b
	if len(s.buf) >= writeBufferSize {
		return s.flush()
	}
	return nil
}

func (s *Sink) appendRecord(b []byte, r domain.Record, c domain.Classification) ([]byte, error) {
	b = append(b, `{"id":`...)
	b = appendString(b, r.ID)
	if r.Kind != "" && r.Kind != domain.KindText {
		b = append(b, `,"kind":`...)
		b = appendString(b, string(r.Kind))
	}
	b = append(b, `,"data":`...)
	b = appendStringBytes(b, r.Data)
	if len(r.Fields) > 0 {
		b = append(b, `,"fields":`...)
		fields, err := s.encodeFields(r.Fields)
		if err != nil {
			return nil, err
		}
		b = append(b, fields...)
	}
	if len(r.Meta) > 0 {
		b = append(b, `,"meta":`...)
		b = s.appendMeta(b, r.Meta)
	}
	b = append(b, `,"category":`...)
	b = appendString(b, c.Category)
	if math.IsNaN(c.Confidence) || math.IsInf(c.Confidence, 0) {
		return nil, fmt.Errorf("unsupported confidence value: %v", c.Confidence)
	}
	b = append(b, `,"confidence":`...)
	b = appendFloat(b, c.Confidence)
	if c.Stage != "" {
		b = append(b, `,"stage":`...)
		b = appendString(b, c.Stage)
	}
	if len(c.Reasons) > 0 {
		b = append(b, `,"reasons":[`...)
		for i, reason := range c.Reasons {
			if i > 0 {
				b = append(b, ',')
			}
			b = append(b, `{"code":`...)
			b = appendString(b, reason.Code)
			if reason.Detail != "" {
				b = append(b, `,"detail":`...)
				b = appendString(b, reason.Detail)
			}
			b = append(b, '}')
		}
		b = append(b, ']')
	}
	return append(b, '}', '\n'), nil
}

// encodeFields serializes the free-form Fields map through encoding/json
// (sorted keys, json.Number passthrough) into a reused scratch buffer.
func (s *Sink) encodeFields(fields map[string]any) ([]byte, error) {
	s.fieldsBuf.Reset()
	if err := s.fieldsEnc.Encode(fields); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(s.fieldsBuf.Bytes(), []byte("\n")), nil
}

// appendMeta serializes the string map with sorted keys, matching
// encoding/json's deterministic map ordering.
func (s *Sink) appendMeta(b []byte, meta map[string]string) []byte {
	s.keys = s.keys[:0]
	for k := range meta {
		s.keys = append(s.keys, k)
	}
	sort.Strings(s.keys)
	b = append(b, '{')
	for i, k := range s.keys {
		if i > 0 {
			b = append(b, ',')
		}
		b = appendString(b, k)
		b = append(b, ':')
		b = appendString(b, meta[k])
	}
	return append(b, '}')
}

// Close implements sink.Sink: flushes buffered output. Safe to call more
// than once.
func (s *Sink) Close() error {
	return s.flush()
}

func (s *Sink) flush() error {
	if len(s.buf) == 0 {
		return nil
	}
	_, err := s.w.Write(s.buf)
	s.buf = s.buf[:0]
	if err != nil {
		return fmt.Errorf("jsonl: flush: %w", err)
	}
	return nil
}

const hexDigits = "0123456789abcdef"

// safeBytes marks the bytes that pass into a JSON string unescaped: ASCII
// 0x20..0x7f except the quote and the backslash. Everything >= utf8.RuneSelf
// stays false so multi-byte runes take the decoding path.
var safeBytes [256]bool

func init() {
	for c := 0x20; c < utf8.RuneSelf; c++ {
		safeBytes[c] = c != '"' && c != '\\'
	}
}

// appendString appends s as a JSON string, matching encoding/json with HTML
// escaping off: quotes, backslashes and control characters are escaped,
// invalid UTF-8 becomes U+FFFD, and U+2028/U+2029 are escaped for JS embedding.
func appendString(b []byte, s string) []byte {
	b = append(b, '"')
	start := 0
	for i := 0; i < len(s); {
		c := s[i]
		if safeBytes[c] {
			i++
			continue
		}
		if c < utf8.RuneSelf {
			b = append(b, s[start:i]...)
			b = appendEscapedByte(b, c)
			i++
			start = i
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size == 1 {
			b = append(b, s[start:i]...)
			b = append(b, '\\', 'u', 'f', 'f', 'f', 'd')
			i += size
			start = i
			continue
		}
		if r == '\u2028' || r == '\u2029' {
			b = append(b, s[start:i]...)
			b = append(b, `\u202`...)
			b = append(b, hexDigits[r&0xF])
			i += size
			start = i
			continue
		}
		i += size
	}
	b = append(b, s[start:]...)
	return append(b, '"')
}

// appendStringBytes is appendString over a byte slice, so the raw payload is
// emitted without converting it to a string first.
func appendStringBytes(b, s []byte) []byte {
	b = append(b, '"')
	start := 0
	for i := 0; i < len(s); {
		c := s[i]
		if safeBytes[c] {
			i++
			continue
		}
		if c < utf8.RuneSelf {
			b = append(b, s[start:i]...)
			b = appendEscapedByte(b, c)
			i++
			start = i
			continue
		}
		r, size := utf8.DecodeRune(s[i:])
		if r == utf8.RuneError && size == 1 {
			b = append(b, s[start:i]...)
			b = append(b, '\\', 'u', 'f', 'f', 'f', 'd')
			i += size
			start = i
			continue
		}
		if r == '\u2028' || r == '\u2029' {
			b = append(b, s[start:i]...)
			b = append(b, `\u202`...)
			b = append(b, hexDigits[r&0xF])
			i += size
			start = i
			continue
		}
		i += size
	}
	b = append(b, s[start:]...)
	return append(b, '"')
}

func appendEscapedByte(b []byte, c byte) []byte {
	switch c {
	case '"':
		return append(b, '\\', '"')
	case '\\':
		return append(b, '\\', '\\')
	case '\b':
		return append(b, '\\', 'b')
	case '\f':
		return append(b, '\\', 'f')
	case '\n':
		return append(b, '\\', 'n')
	case '\r':
		return append(b, '\\', 'r')
	case '\t':
		return append(b, '\\', 't')
	default:
		return append(b, '\\', 'u', '0', '0', hexDigits[c>>4], hexDigits[c&0xF])
	}
}

// appendFloat matches encoding/json's float formatting: fixed notation in the
// human range, exponent notation outside it, with the exponent normalized.
func appendFloat(b []byte, f float64) []byte {
	if f == 1 {
		return append(b, '1')
	}
	if f == 0 && !math.Signbit(f) {
		return append(b, '0')
	}
	abs := math.Abs(f)
	format := byte('f')
	if abs != 0 && (abs < 1e-6 || abs >= 1e21) {
		format = 'e'
	}
	b = strconv.AppendFloat(b, f, format, -1, 64)
	if format == 'e' {
		if n := len(b); n >= 4 && b[n-4] == 'e' && b[n-3] == '-' && b[n-2] == '0' {
			b[n-2] = b[n-1]
			b = b[:n-1]
		}
	}
	return b
}

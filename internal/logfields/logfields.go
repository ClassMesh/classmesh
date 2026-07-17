// Package logfields extracts common fields from a text log line, best-effort:
// a leading timestamp, a level token, and logfmt key=value pairs. It is an
// optional adapter that sits above a text source; a line it cannot read still
// flows through untouched, with its raw bytes intact.
package logfields

import (
	"bytes"
	"strings"
	"time"

	domain "github.com/ClassMesh/classmesh"
)

// maxLevelLen is the longest recognized level word ("warning"); a longer ASCII
// token cannot be a level, so it is rejected without lowercasing.
const maxLevelLen = 7

var levels = map[string]bool{
	"trace": true, "debug": true, "info": true, "warn": true,
	"warning": true, "error": true, "fatal": true, "panic": true,
}

// Parse reads a log line into a field map, best-effort. It recognizes logfmt
// key=value pairs anywhere in the line, a leading RFC3339 timestamp, and a
// leading level word (info, error, warn, ...). It never errors; an
// unrecognized line yields nil, and nothing is allocated until a field is found.
func Parse(line []byte) map[string]any {
	fields := parseLogfmt(line)

	rest := bytes.TrimSpace(line)
	if tok, after, ok := leadingToken(rest); ok && isTimestamp(tok) {
		fields = putIfAbsent(fields, "timestamp", string(tok))
		rest = after
	}
	if tok, _, ok := leadingToken(rest); ok {
		if lvl, ok := levelWord(tok); ok {
			fields = putIfAbsent(fields, "level", lvl)
		}
	}

	if len(fields) == 0 {
		return nil
	}
	return fields
}

// Enrich returns r with the fields parsed from its Data merged into Fields and
// Kind set to KindLog. Existing Fields win over parsed ones, and the raw Data
// is left untouched. A line nothing is extracted from returns r unchanged, so
// it stays a usable record.
func Enrich(r domain.Record) domain.Record {
	parsed := Parse(r.Data)
	if len(parsed) == 0 {
		return r
	}
	if r.Fields == nil {
		r.Fields = parsed
		r.Kind = domain.KindLog
		return r
	}
	merged := make(map[string]any, len(r.Fields)+len(parsed))
	for k, v := range parsed {
		merged[k] = v
	}
	for k, v := range r.Fields {
		merged[k] = v
	}
	r.Fields = merged
	r.Kind = domain.KindLog
	return r
}

func putIfAbsent(fields map[string]any, key, value string) map[string]any {
	if fields == nil {
		fields = make(map[string]any, 4)
	}
	if _, exists := fields[key]; !exists {
		fields[key] = value
	}
	return fields
}

// parseLogfmt scans line for key=value pairs. It stays on the raw bytes until
// the first pair is found, then materializes the line into one string and slices
// keys and unquoted values out of it as substrings (indices into line and the
// copy coincide), so a line with no pairs allocates nothing.
func parseLogfmt(line []byte) map[string]any {
	var out map[string]any
	var s string
	i := 0
	for i < len(line) {
		for i < len(line) && isSpace(line[i]) {
			i++
		}
		keyStart := i
		for i < len(line) && line[i] != '=' && !isSpace(line[i]) {
			i++
		}
		if i >= len(line) || line[i] != '=' || i == keyStart {
			for i < len(line) && !isSpace(line[i]) {
				i++
			}
			continue
		}
		if out == nil {
			out = make(map[string]any, 4)
			s = string(line)
		}
		key := s[keyStart:i]
		i++
		out[key] = readValue(s, &i)
	}
	return out
}

// readValue reads a logfmt value starting at *i: a double-quoted string (with
// \" and \\ unescaped) or an unquoted run up to the next space. An unquoted
// value is returned as a substring, so it does not allocate.
func readValue(s string, i *int) string {
	if *i < len(s) && s[*i] == '"' {
		*i++
		var b strings.Builder
		for *i < len(s) && s[*i] != '"' {
			if s[*i] == '\\' && *i+1 < len(s) && (s[*i+1] == '"' || s[*i+1] == '\\') {
				*i++
			}
			b.WriteByte(s[*i])
			*i++
		}
		if *i < len(s) {
			*i++
		}
		return b.String()
	}
	start := *i
	for *i < len(s) && !isSpace(s[*i]) {
		*i++
	}
	return s[start:*i]
}

func isSpace(b byte) bool { return b == ' ' || b == '\t' }

func leadingToken(s []byte) (tok, rest []byte, ok bool) {
	s = bytes.TrimLeft(s, " \t")
	if len(s) == 0 {
		return nil, nil, false
	}
	if i := bytes.IndexAny(s, " \t"); i >= 0 {
		return s[:i], s[i:], true
	}
	return s, nil, true
}

// isTimestamp reports whether tok is an RFC3339 timestamp. A cheap layout probe
// rejects the ordinary token before the allocating time.Parse is reached, so a
// non-timestamp line pays nothing.
func isTimestamp(tok []byte) bool {
	if !looksLikeRFC3339(tok) {
		return false
	}
	for _, layout := range []string{time.RFC3339, time.RFC3339Nano} {
		if _, err := time.Parse(layout, string(tok)); err == nil {
			return true
		}
	}
	return false
}

// looksLikeRFC3339 checks the fixed date-time skeleton (2006-01-02T15:04:05...)
// by position: the digit runs are left for time.Parse, only the separators are
// probed here.
func looksLikeRFC3339(tok []byte) bool {
	if len(tok) < 20 {
		return false
	}
	if tok[4] != '-' || tok[7] != '-' || tok[13] != ':' || tok[16] != ':' {
		return false
	}
	return tok[10] == 'T' || tok[10] == 't'
}

// levelWord lowercases the trimmed token on the stack when it is ASCII; a token
// with a non-ASCII byte takes the allocating strings.ToLower path so Unicode
// case mappings (İNFO -> info) are recognized exactly as before.
func levelWord(tok []byte) (string, bool) {
	t := bytes.Trim(tok, "[]():")
	if len(t) == 0 {
		return "", false
	}
	if !isASCII(t) {
		lowered := strings.ToLower(string(t))
		if levels[lowered] {
			return lowered, true
		}
		return "", false
	}
	if len(t) > maxLevelLen {
		return "", false
	}
	var buf [maxLevelLen]byte
	for i := 0; i < len(t); i++ {
		buf[i] = lowerASCII(t[i])
	}
	lowered := buf[:len(t)]
	if levels[string(lowered)] {
		return string(lowered), true
	}
	return "", false
}

func isASCII(s []byte) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

func lowerASCII(b byte) byte {
	if b >= 'A' && b <= 'Z' {
		return b + ('a' - 'A')
	}
	return b
}

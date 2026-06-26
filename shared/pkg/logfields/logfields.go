// Package logfields extracts common fields from a text log line, best-effort:
// a leading timestamp, a level token, and logfmt key=value pairs. It is an
// optional adapter that sits above a text source — a line it cannot read still
// flows through untouched, with its raw bytes intact.
package logfields

import (
	"strings"
	"time"

	"github.com/ClassMesh/classmesh/shared/pkg/domain"
)

var levels = map[string]bool{
	"trace": true, "debug": true, "info": true, "warn": true,
	"warning": true, "error": true, "fatal": true, "panic": true,
}

// Parse reads a log line into a field map, best-effort. It recognizes logfmt
// key=value pairs anywhere in the line, a leading RFC3339 timestamp, and a
// leading level word (info, error, warn, ...). It never errors; an
// unrecognized line yields nil.
func Parse(line []byte) map[string]any {
	s := string(line)
	fields := parseLogfmt(s)

	rest := strings.TrimSpace(s)
	if tok, after, ok := leadingToken(rest); ok && isTimestamp(tok) {
		if _, exists := fields["timestamp"]; !exists {
			fields["timestamp"] = tok
		}
		rest = after
	}
	if tok, _, ok := leadingToken(rest); ok {
		if lvl, ok := levelWord(tok); ok {
			if _, exists := fields["level"]; !exists {
				fields["level"] = lvl
			}
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

func parseLogfmt(s string) map[string]any {
	out := map[string]any{}
	i := 0
	for i < len(s) {
		for i < len(s) && s[i] == ' ' {
			i++
		}
		keyStart := i
		for i < len(s) && s[i] != '=' && s[i] != ' ' {
			i++
		}
		if i >= len(s) || s[i] != '=' || i == keyStart {
			for i < len(s) && s[i] != ' ' {
				i++
			}
			continue
		}
		key := s[keyStart:i]
		i++ // consume '='
		out[key] = readValue(s, &i)
	}
	return out
}

// readValue reads a logfmt value starting at *i: a double-quoted string (with
// \" and \\ unescaped) or an unquoted run up to the next space.
func readValue(s string, i *int) string {
	if *i < len(s) && s[*i] == '"' {
		*i++
		var b strings.Builder
		for *i < len(s) && s[*i] != '"' {
			if s[*i] == '\\' && *i+1 < len(s) && (s[*i+1] == '"' || s[*i+1] == '\\') {
				*i++ // consume the backslash; write the escaped char below
			}
			b.WriteByte(s[*i])
			*i++
		}
		if *i < len(s) {
			*i++ // consume closing quote
		}
		return b.String()
	}
	start := *i
	for *i < len(s) && s[*i] != ' ' {
		*i++
	}
	return s[start:*i]
}

func leadingToken(s string) (tok, rest string, ok bool) {
	s = strings.TrimLeft(s, " \t")
	if s == "" {
		return "", "", false
	}
	if i := strings.IndexAny(s, " \t"); i >= 0 {
		return s[:i], s[i:], true
	}
	return s, "", true
}

func isTimestamp(tok string) bool {
	for _, layout := range []string{time.RFC3339, time.RFC3339Nano} {
		if _, err := time.Parse(layout, tok); err == nil {
			return true
		}
	}
	return false
}

func levelWord(tok string) (string, bool) {
	t := strings.ToLower(strings.Trim(tok, "[]():"))
	if levels[t] {
		return t, true
	}
	return "", false
}

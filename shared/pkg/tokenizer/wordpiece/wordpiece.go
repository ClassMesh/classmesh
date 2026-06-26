// Package wordpiece implements BERT-style WordPiece tokenization in pure Go,
// with no cgo and no external tokenizer runtime. It turns text into the token
// IDs an ONNX BERT/MiniLM classifier expects: basic tokenization (Unicode
// cleanup, optional lowercasing and accent stripping, punctuation and CJK
// splitting) followed by greedy longest-match-first subword segmentation
// against a fixed vocabulary, wrapped with [CLS] and [SEP].
//
// It exists so the in-process model stage can stay a single static binary
// (CGO_ENABLED=0): the common alternative, the rust tokenizers library, needs
// cgo and a statically linked archive. We own this instead.
package wordpiece

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// Default special tokens, matching the conventional BERT vocabulary.
const (
	DefaultUnknown    = "[UNK]"
	DefaultClassify   = "[CLS]"
	DefaultSeparator  = "[SEP]"
	defaultMaxPerWord = 100
)

// Encoding is the model-ready result of tokenizing one input. The slices are
// parallel and always the same length: IDs feeds the model's input_ids, Mask
// its attention_mask (1 for every real token here, since we do not pad), and
// TypeIDs its token_type_ids (0 throughout for a single sequence). Tokens
// holds the matching string pieces, for debugging and tests.
type Encoding struct {
	IDs     []int32
	Mask    []int32
	TypeIDs []int32
	Tokens  []string
}

// Tokenizer encodes text against a fixed vocabulary. Build one with New,
// Parse, or Load; it is read-only and safe for concurrent use after
// construction.
type Tokenizer struct {
	vocab map[string]int32

	unk, cls, sep       string
	unkID, clsID, sepID int32

	lower      bool
	maxLen     int
	maxPerWord int
}

// Option configures a Tokenizer at construction.
type Option func(*Tokenizer)

// Lowercase controls whether input is lowercased and accent-stripped before
// matching (BERT "uncased" behaviour). Defaults to true.
func Lowercase(on bool) Option { return func(t *Tokenizer) { t.lower = on } }

// MaxLen caps the total encoded length including [CLS] and [SEP]; longer input
// is truncated. Zero (the default) means no limit. Must be at least 2.
func MaxLen(n int) Option { return func(t *Tokenizer) { t.maxLen = n } }

// MaxCharsPerWord sets the length above which a single whitespace-delimited
// word is emitted as the unknown token rather than segmented. Defaults to 100.
func MaxCharsPerWord(n int) Option { return func(t *Tokenizer) { t.maxPerWord = n } }

// SpecialTokens overrides the unknown, classify, and separator token strings
// looked up in the vocabulary. Defaults to [UNK], [CLS], [SEP].
func SpecialTokens(unk, cls, sep string) Option {
	return func(t *Tokenizer) { t.unk, t.cls, t.sep = unk, cls, sep }
}

// New builds a Tokenizer from a vocabulary mapping each token to its ID. The
// special tokens must be present in the vocabulary.
func New(vocab map[string]int32, opts ...Option) (*Tokenizer, error) {
	if len(vocab) == 0 {
		return nil, errors.New("wordpiece: vocab is empty")
	}
	owned := make(map[string]int32, len(vocab))
	for tok, id := range vocab {
		owned[tok] = id
	}
	t := &Tokenizer{
		vocab:      owned,
		unk:        DefaultUnknown,
		cls:        DefaultClassify,
		sep:        DefaultSeparator,
		lower:      true,
		maxPerWord: defaultMaxPerWord,
	}
	for _, o := range opts {
		o(t)
	}
	if t.maxLen != 0 && t.maxLen < 2 {
		return nil, fmt.Errorf("wordpiece: max length %d leaves no room for [CLS] and [SEP]", t.maxLen)
	}
	if t.maxPerWord < 1 {
		return nil, fmt.Errorf("wordpiece: max chars per word %d must be positive", t.maxPerWord)
	}
	var ok bool
	if t.unkID, ok = vocab[t.unk]; !ok {
		return nil, fmt.Errorf("wordpiece: vocab missing unknown token %q", t.unk)
	}
	if t.clsID, ok = vocab[t.cls]; !ok {
		return nil, fmt.Errorf("wordpiece: vocab missing classify token %q", t.cls)
	}
	if t.sepID, ok = vocab[t.sep]; !ok {
		return nil, fmt.Errorf("wordpiece: vocab missing separator token %q", t.sep)
	}
	return t, nil
}

// Parse builds a Tokenizer from a vocab.txt stream: one token per line, the
// zero-based line number being its ID.
func Parse(r io.Reader, opts ...Option) (*Tokenizer, error) {
	vocab := make(map[string]int32)
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var i int32
	for sc.Scan() {
		tok := strings.TrimRight(sc.Text(), "\r")
		if _, dup := vocab[tok]; !dup {
			vocab[tok] = i
		}
		i++
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("wordpiece: read vocab: %w", err)
	}
	return New(vocab, opts...)
}

// Load builds a Tokenizer from a vocab.txt file.
func Load(path string, opts ...Option) (*Tokenizer, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("wordpiece: %w", err)
	}
	defer func() { _ = f.Close() }()
	return Parse(f, opts...)
}

// Encode tokenizes text into model-ready IDs, wrapping the subword pieces with
// [CLS] and [SEP] and truncating to MaxLen when set.
func (t *Tokenizer) Encode(text string) Encoding {
	var pieces []string
	var buf []byte
	for _, word := range t.basicTokenize(text) {
		pieces, buf = t.wordpiece(pieces, buf, word)
	}
	if t.maxLen > 0 && len(pieces) > t.maxLen-2 {
		pieces = pieces[:t.maxLen-2]
	}

	n := len(pieces) + 2
	enc := Encoding{
		IDs:     make([]int32, 0, n),
		Mask:    make([]int32, 0, n),
		TypeIDs: make([]int32, 0, n),
		Tokens:  make([]string, 0, n),
	}
	add := func(tok string, id int32) {
		enc.Tokens = append(enc.Tokens, tok)
		enc.IDs = append(enc.IDs, id)
		enc.Mask = append(enc.Mask, 1)
		enc.TypeIDs = append(enc.TypeIDs, 0)
	}
	add(t.cls, t.clsID)
	for _, p := range pieces {
		add(p, t.vocab[p])
	}
	add(t.sep, t.sepID)
	return enc
}

// basicTokenize cleans and splits text into whitespace-delimited words,
// isolating punctuation and CJK characters and, when configured, lowercasing
// and stripping accents. It mirrors BERT's BasicTokenizer.
func (t *Tokenizer) basicTokenize(text string) []string {
	text = cleanText(text)
	text = spaceCJK(text)
	var out []string
	for _, word := range strings.Fields(text) {
		if t.lower {
			word = stripAccents(strings.ToLower(word))
		}
		out = append(out, splitOnPunctuation(word)...)
	}
	return out
}

// wordpiece segments word into subword tokens with greedy longest-match-first
// lookup, appending them to dst. buf is reused scratch for the "##"-prefixed
// continuation lookups, so candidates do not allocate. A word with any
// unmatchable piece, or longer than maxPerWord characters, becomes the unknown
// token. It returns the grown dst and buf.
func (t *Tokenizer) wordpiece(dst []string, buf []byte, word string) ([]string, []byte) {
	if word == "" {
		return dst, buf
	}
	if utf8.RuneCountInString(word) > t.maxPerWord {
		return append(dst, t.unk), buf
	}
	mark := len(dst)
	for start := 0; start < len(word); {
		end := len(word)
		match, matchEnd := "", -1
		for start < end {
			if start == 0 {
				if _, ok := t.vocab[word[start:end]]; ok {
					match, matchEnd = word[start:end], end
					break
				}
			} else {
				buf = append(buf[:0], "##"...)
				buf = append(buf, word[start:end]...)
				if _, ok := t.vocab[string(buf)]; ok {
					match, matchEnd = string(buf), end
					break
				}
			}
			_, size := utf8.DecodeLastRuneInString(word[start:end])
			end -= size
		}
		if matchEnd < 0 {
			return append(dst[:mark], t.unk), buf
		}
		dst = append(dst, match)
		start = matchEnd
	}
	return dst, buf
}

// cleanText drops control characters and replacement runes and collapses every
// kind of whitespace to a plain space.
func cleanText(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r == 0 || r == 0xFFFD || isControl(r) {
			continue
		}
		if isWhitespace(r) {
			b.WriteByte(' ')
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// spaceCJK pads CJK ideographs with spaces so each becomes its own token.
func spaceCJK(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if isCJK(r) {
			b.WriteByte(' ')
			b.WriteRune(r)
			b.WriteByte(' ')
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// stripAccents removes combining marks after NFD decomposition.
func stripAccents(s string) string {
	d := norm.NFD.String(s)
	var b strings.Builder
	b.Grow(len(d))
	for _, r := range d {
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// splitOnPunctuation breaks a word so each punctuation rune is its own token.
func splitOnPunctuation(s string) []string {
	var out []string
	var cur []rune
	flush := func() {
		if len(cur) > 0 {
			out = append(out, string(cur))
			cur = cur[:0]
		}
	}
	for _, r := range s {
		if isPunctuation(r) {
			flush()
			out = append(out, string(r))
		} else {
			cur = append(cur, r)
		}
	}
	flush()
	return out
}

func isWhitespace(r rune) bool {
	switch r {
	case ' ', '\t', '\n', '\r':
		return true
	}
	return unicode.Is(unicode.Zs, r)
}

func isControl(r rune) bool {
	switch r {
	case '\t', '\n', '\r':
		return false
	}
	return unicode.IsControl(r) || unicode.Is(unicode.Cf, r)
}

// isPunctuation treats the ASCII punctuation ranges and every Unicode P*
// category as punctuation, matching BERT's _is_punctuation.
func isPunctuation(r rune) bool {
	if (r >= 33 && r <= 47) || (r >= 58 && r <= 64) ||
		(r >= 91 && r <= 96) || (r >= 123 && r <= 126) {
		return true
	}
	return unicode.IsPunct(r)
}

// isCJK reports whether r is in one of the CJK ideograph blocks BERT isolates.
func isCJK(r rune) bool {
	switch {
	case r >= 0x4E00 && r <= 0x9FFF,
		r >= 0x3400 && r <= 0x4DBF,
		r >= 0x20000 && r <= 0x2A6DF,
		r >= 0x2A700 && r <= 0x2B73F,
		r >= 0x2B740 && r <= 0x2B81F,
		r >= 0x2B820 && r <= 0x2CEAF,
		r >= 0xF900 && r <= 0xFAFF,
		r >= 0x2F800 && r <= 0x2FA1F:
		return true
	}
	return false
}

// Package wordpiece implements BERT-style WordPiece tokenization in pure Go,
// with no cgo and no external tokenizer runtime. It turns text into the token
// IDs a BERT-vocabulary model expects: basic tokenization (Unicode
// cleanup, optional lowercasing and accent stripping, punctuation and CJK
// splitting) followed by greedy longest-match-first subword segmentation
// against a fixed vocabulary, wrapped with [CLS] and [SEP].
//
// It is intended to feed a future in-process model stage so the binary can
// stay CGO_ENABLED=0; no such stage is wired yet. The common alternative, the
// rust tokenizers library, needs cgo and a statically linked archive.
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
	vocab  map[string]int32
	tokens []string // ID-to-token reverse table, or nil when reverseVocab declines

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
	var maxID int32
	for tok, id := range vocab {
		owned[tok] = id
		if id > maxID {
			maxID = id
		}
	}
	t := &Tokenizer{
		vocab:      owned,
		tokens:     reverseVocab(owned, maxID),
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

// reverseVocab builds an ID-to-token table so subword matches reuse interned
// strings. Negative, sparse, or colliding IDs and empty tokens return nil;
// callers fall back to constructing the string, which is always safe.
func reverseVocab(vocab map[string]int32, maxID int32) []string {
	if maxID < 0 || int64(maxID)+1 > int64(2*len(vocab)) {
		return nil
	}
	rev := make([]string, maxID+1)
	for tok, id := range vocab {
		if id < 0 || tok == "" || rev[id] != "" {
			return nil
		}
		rev[id] = tok
	}
	return rev
}

// Encode tokenizes text into model-ready IDs, wrapping the subword pieces with
// [CLS] and [SEP] and truncating to MaxLen when set. The returned IDs, Mask,
// and TypeIDs share one backing array, each capped to its own segment so the
// appends below cannot cross into a neighbour.
func (t *Tokenizer) Encode(text string) Encoding {
	words := t.basicTokenize(text)
	var pieceScratch [32]string
	var bufScratch [128]byte
	pieces, buf := pieceScratch[:0], bufScratch[:0]
	for _, word := range words {
		pieces, buf = t.wordpiece(pieces, buf, word)
	}
	if t.maxLen > 0 && len(pieces) > t.maxLen-2 {
		pieces = pieces[:t.maxLen-2]
	}

	n := len(pieces) + 2
	ints := make([]int32, 3*n)
	enc := Encoding{
		IDs:     ints[0:0:n],
		Mask:    ints[n : n : 2*n],
		TypeIDs: ints[2*n : 2*n : 3*n],
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
// and stripping accents. It mirrors BERT's BasicTokenizer. cleanAndSpaceCJK
// has already collapsed every whitespace run to a single ASCII space, so words
// are the runs between spaces.
func (t *Tokenizer) basicTokenize(text string) []string {
	text = cleanAndSpaceCJK(text)
	out := make([]string, 0, strings.Count(text, " ")+1)
	start := 0
	for i := 0; i <= len(text); i++ {
		if i < len(text) && text[i] != ' ' {
			continue
		}
		if start < i {
			word := text[start:i]
			if t.lower {
				word = stripAccents(strings.ToLower(word))
			}
			out = appendPunctuationSplit(out, word)
		}
		start = i + 1
	}
	if len(out) == 0 {
		return nil
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
			var id int32
			var ok bool
			if start == 0 {
				id, ok = t.vocab[word[start:end]]
			} else {
				buf = append(buf[:0], "##"...)
				buf = append(buf, word[start:end]...)
				id, ok = t.vocab[string(buf)]
			}
			if ok {
				switch {
				case t.tokens != nil:
					match = t.tokens[id]
				case start == 0:
					match = word[start:end]
				default:
					match = string(buf)
				}
				matchEnd = end
				break
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

// cleanAndSpaceCJK folds BERT's cleanup and CJK-spacing passes into one:
// controls and replacement runes drop, every unicode.IsSpace rune becomes a
// plain space (preserving strings.Fields split semantics), and CJK ideographs
// are space-padded into standalone tokens.
func cleanAndSpaceCJK(s string) string {
	simple := true
	for _, r := range s {
		if r == 0 || r == 0xFFFD || isControl(r) || (isWhitespace(r) && r != ' ') ||
			(r >= utf8.RuneSelf && unicode.IsSpace(r)) || isCJK(r) {
			simple = false
			break
		}
	}
	if simple {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == 0 || r == 0xFFFD || isControl(r):
			continue
		case isWhitespace(r) || (r >= utf8.RuneSelf && unicode.IsSpace(r)):
			b.WriteByte(' ')
		case isCJK(r):
			b.WriteByte(' ')
			b.WriteRune(r)
			b.WriteByte(' ')
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// stripAccents removes combining marks after NFD decomposition. Pure-ASCII
// input has no combining marks and is unchanged by NFD, so it is returned as
// is without normalizing.
func stripAccents(s string) string {
	if isASCII(s) {
		return s
	}
	var normScratch, outScratch [128]byte
	d := norm.NFD.AppendString(normScratch[:0], s)
	out := outScratch[:0]
	for i := 0; i < len(d); {
		r, size := utf8.DecodeRune(d[i:])
		if !unicode.Is(unicode.Mn, r) {
			out = append(out, d[i:i+size]...)
		}
		i += size
	}
	return string(out)
}

// appendPunctuationSplit appends s to dst broken so each punctuation rune is
// its own token. The pieces are substrings of s, so no per-token allocation
// occurs beyond growing dst.
func appendPunctuationSplit(dst []string, s string) []string {
	start := 0
	for i, r := range s {
		if isPunctuation(r) {
			if start < i {
				dst = append(dst, s[start:i])
			}
			dst = append(dst, s[i:i+utf8.RuneLen(r)])
			start = i + utf8.RuneLen(r)
		}
	}
	if start < len(s) {
		dst = append(dst, s[start:])
	}
	return dst
}

// isASCII reports whether s contains only ASCII bytes.
func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}

func isWhitespace(r rune) bool {
	switch r {
	case ' ', '\t', '\n', '\r':
		return true
	}
	if r < utf8.RuneSelf {
		return false
	}
	return unicode.Is(unicode.Zs, r)
}

func isControl(r rune) bool {
	switch r {
	case '\t', '\n', '\r':
		return false
	}
	if r < utf8.RuneSelf {
		return r < 0x20 || r == 0x7F
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
	if r < utf8.RuneSelf {
		return false
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

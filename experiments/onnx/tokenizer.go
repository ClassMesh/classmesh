package main

import (
	"bufio"
	"os"
	"strings"
	"unicode"
)

const (
	clsToken      = "[CLS]"
	sepToken      = "[SEP]"
	unkToken      = "[UNK]"
	padToken      = "[PAD]"
	subwordPrefix = "##"
	maxWordChars  = 100
)

// Tokenizer is a pure-Go BERT WordPiece tokenizer for uncased models.
type Tokenizer struct {
	vocab map[string]int64
}

// LoadTokenizer reads a BERT vocab.txt where line number is the token id.
func LoadTokenizer(vocabPath string) (*Tokenizer, error) {
	f, err := os.Open(vocabPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	vocab := make(map[string]int64)
	scanner := bufio.NewScanner(f)
	var id int64
	for scanner.Scan() {
		vocab[strings.TrimRight(scanner.Text(), "\r\n")] = id
		id++
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return &Tokenizer{vocab: vocab}, nil
}

// Encode returns padded input ids, attention mask and token type ids for a sentence.
func (t *Tokenizer) Encode(text string, seqLen int) (ids, mask, types []int64) {
	pieces := []int64{t.vocab[clsToken]}
	for _, word := range basicSplit(text) {
		pieces = append(pieces, t.wordPiece(word)...)
	}
	pieces = append(pieces, t.vocab[sepToken])
	if len(pieces) > seqLen {
		pieces = pieces[:seqLen]
		pieces[seqLen-1] = t.vocab[sepToken]
	}
	ids = make([]int64, seqLen)
	mask = make([]int64, seqLen)
	types = make([]int64, seqLen)
	pad := t.vocab[padToken]
	for i := range ids {
		if i < len(pieces) {
			ids[i] = pieces[i]
			mask[i] = 1
		} else {
			ids[i] = pad
		}
	}
	return ids, mask, types
}

// wordPiece greedily splits a single word into the longest matching subwords.
func (t *Tokenizer) wordPiece(word string) []int64 {
	runes := []rune(word)
	if len(runes) > maxWordChars {
		return []int64{t.vocab[unkToken]}
	}
	var out []int64
	start := 0
	for start < len(runes) {
		end := len(runes)
		var cur int64 = -1
		for start < end {
			sub := string(runes[start:end])
			if start > 0 {
				sub = subwordPrefix + sub
			}
			if id, ok := t.vocab[sub]; ok {
				cur = id
				break
			}
			end--
		}
		if cur < 0 {
			return []int64{t.vocab[unkToken]}
		}
		out = append(out, cur)
		start = end
	}
	return out
}

// basicSplit lowercases text and splits on whitespace and punctuation.
func basicSplit(text string) []string {
	var words []string
	var b strings.Builder
	flush := func() {
		if b.Len() > 0 {
			words = append(words, b.String())
			b.Reset()
		}
	}
	for _, r := range strings.ToLower(text) {
		switch {
		case unicode.IsSpace(r):
			flush()
		case isPunct(r):
			flush()
			words = append(words, string(r))
		default:
			b.WriteRune(r)
		}
	}
	flush()
	return words
}

// isPunct reports whether r is treated as a standalone punctuation token.
func isPunct(r rune) bool {
	if unicode.IsPunct(r) || unicode.IsSymbol(r) {
		return true
	}
	return false
}

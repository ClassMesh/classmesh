package wordpiece

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// bertVocab is the fixture from Google's BERT tokenization_test.py, in order
// so each token's ID is its index.
var bertVocab = []string{
	"[UNK]", "[CLS]", "[SEP]", "want", "##want", "##ed",
	"wa", "un", "runn", "##ing", ",", "low", "lowest",
}

func vocabMap(tokens []string) map[string]int32 {
	m := make(map[string]int32, len(tokens))
	for i, tok := range tokens {
		m[tok] = int32(i)
	}
	return m
}

func mustNew(t *testing.T, tokens []string, opts ...Option) *Tokenizer {
	t.Helper()
	tok, err := New(vocabMap(tokens), opts...)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return tok
}

func TestBasicTokenize(t *testing.T) {
	tok := mustNew(t, bertVocab)
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{"punctuation", "Hello, World!", []string{"hello", ",", "world", "!"}},
		{"accents", "Héllo", []string{"hello"}},
		{"whitespace and control", "\tHello\x00 \n World ", []string{"hello", "world"}},
		{"cjk isolated", "ah博推zz", []string{"ah", "博", "推", "zz"}},
		{"empty", "   ", nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tok.basicTokenize(tc.in); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("basicTokenize(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestBasicTokenizeNoLowercase(t *testing.T) {
	tok := mustNew(t, bertVocab, Lowercase(false))
	got := tok.basicTokenize("Héllo, World")
	want := []string{"Héllo", ",", "World"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("basicTokenize = %v, want %v (casing and accents preserved)", got, want)
	}
}

func TestWordpiece(t *testing.T) {
	tok := mustNew(t, bertVocab)
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{"empty", "", nil},
		{"two words segmented", "unwanted", []string{"un", "##want", "##ed"}},
		{"running", "runn", []string{"runn"}},
		{"unmatchable becomes unk", "unwantedX", []string{"[UNK]"}},
		{"first piece missing", "X", []string{"[UNK]"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tok.wordpiece(tc.in); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("wordpiece(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestWordpieceLongWordIsUnknown(t *testing.T) {
	tok := mustNew(t, bertVocab, MaxCharsPerWord(4))
	if got := tok.wordpiece("running"); !reflect.DeepEqual(got, []string{"[UNK]"}) {
		t.Fatalf("wordpiece(long) = %v, want [[UNK]]", got)
	}
}

// TestEncodeCanonical runs the full pipeline against the BERT FullTokenizer
// vector: accented, mixed-case, punctuation-joined input.
func TestEncodeCanonical(t *testing.T) {
	tok := mustNew(t, bertVocab)
	enc := tok.Encode("UNwantéd,running")

	wantTokens := []string{"[CLS]", "un", "##want", "##ed", ",", "runn", "##ing", "[SEP]"}
	if !reflect.DeepEqual(enc.Tokens, wantTokens) {
		t.Fatalf("Tokens = %v, want %v", enc.Tokens, wantTokens)
	}
	wantIDs := []int32{1, 7, 4, 5, 10, 8, 9, 2}
	if !reflect.DeepEqual(enc.IDs, wantIDs) {
		t.Fatalf("IDs = %v, want %v", enc.IDs, wantIDs)
	}
	assertParallel(t, enc)
}

func TestEncodeEmpty(t *testing.T) {
	tok := mustNew(t, bertVocab)
	enc := tok.Encode("")
	if !reflect.DeepEqual(enc.Tokens, []string{"[CLS]", "[SEP]"}) {
		t.Fatalf("Tokens = %v, want [[CLS] [SEP]]", enc.Tokens)
	}
	if !reflect.DeepEqual(enc.IDs, []int32{1, 2}) {
		t.Fatalf("IDs = %v, want [1 2]", enc.IDs)
	}
	assertParallel(t, enc)
}

func TestEncodeTruncates(t *testing.T) {
	tok := mustNew(t, bertVocab, MaxLen(4))
	enc := tok.Encode("unwanted unwanted unwanted") // many pieces
	if len(enc.IDs) != 4 {
		t.Fatalf("len(IDs) = %d, want 4 (MaxLen)", len(enc.IDs))
	}
	if enc.Tokens[0] != "[CLS]" || enc.Tokens[len(enc.Tokens)-1] != "[SEP]" {
		t.Fatalf("Tokens = %v, want [CLS] ... [SEP]", enc.Tokens)
	}
	assertParallel(t, enc)
}

func TestEncodeMaskAndTypeIDs(t *testing.T) {
	tok := mustNew(t, bertVocab)
	enc := tok.Encode("unwanted running")
	for i, m := range enc.Mask {
		if m != 1 {
			t.Fatalf("Mask[%d] = %d, want 1", i, m)
		}
	}
	for i, ty := range enc.TypeIDs {
		if ty != 0 {
			t.Fatalf("TypeIDs[%d] = %d, want 0", i, ty)
		}
	}
}

func assertParallel(t *testing.T, enc Encoding) {
	t.Helper()
	n := len(enc.IDs)
	if len(enc.Mask) != n || len(enc.TypeIDs) != n || len(enc.Tokens) != n {
		t.Fatalf("parallel slices differ: ids=%d mask=%d types=%d tokens=%d",
			n, len(enc.Mask), len(enc.TypeIDs), len(enc.Tokens))
	}
}

func TestNewValidation(t *testing.T) {
	tests := []struct {
		name  string
		vocab map[string]int32
		opts  []Option
	}{
		{"empty vocab", map[string]int32{}, nil},
		{"missing unk", vocabMap([]string{"[CLS]", "[SEP]"}), nil},
		{"missing cls", vocabMap([]string{"[UNK]", "[SEP]"}), nil},
		{"missing sep", vocabMap([]string{"[UNK]", "[CLS]"}), nil},
		{"maxlen too small", vocabMap(bertVocab), []Option{MaxLen(1)}},
		{"maxperword zero", vocabMap(bertVocab), []Option{MaxCharsPerWord(0)}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := New(tc.vocab, tc.opts...); err == nil {
				t.Fatalf("New() error = nil, want non-nil")
			}
		})
	}
}

func TestParseAssignsLineNumbersAsIDs(t *testing.T) {
	const vocab = "[UNK]\n[CLS]\n[SEP]\nhello\n##world\n"
	tok, err := Parse(strings.NewReader(vocab))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	for tokn, want := range map[string]int32{"[UNK]": 0, "[CLS]": 1, "[SEP]": 2, "hello": 3, "##world": 4} {
		if got := tok.vocab[tokn]; got != want {
			t.Fatalf("vocab[%q] = %d, want %d", tokn, got, want)
		}
	}
}

func TestLoadFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vocab.txt")
	if err := os.WriteFile(path, []byte(strings.Join(bertVocab, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("write vocab: %v", err)
	}
	tok, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	enc := tok.Encode("unwanted")
	if !reflect.DeepEqual(enc.Tokens, []string{"[CLS]", "un", "##want", "##ed", "[SEP]"}) {
		t.Fatalf("Tokens = %v", enc.Tokens)
	}
}

func TestLoadMissingFile(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "nope.txt")); err == nil {
		t.Fatalf("Load() error = nil, want non-nil")
	}
}

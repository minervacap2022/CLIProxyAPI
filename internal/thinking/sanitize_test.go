package thinking

import (
	"bytes"
	"strings"
	"testing"
)

func TestSanitizeMessagesThinking_RemovesEmptyAndMissingSignatures(t *testing.T) {
	input := []byte(`{"messages":[{"role":"assistant","content":[` +
		`{"type":"thinking","thinking":"no-sig"},` +
		`{"type":"thinking","thinking":"empty-sig","signature":""},` +
		`{"type":"thinking","thinking":"whitespace-sig","signature":"   "},` +
		`{"type":"thinking","thinking":"number-sig","signature":123},` +
		`{"type":"thinking","thinking":"keep","signature":"valid-signature-xyz"},` +
		`{"type":"text","text":"hello"}` +
		`]}]}`)

	out := SanitizeMessagesThinking(input)

	for _, s := range []string{"no-sig", "empty-sig", "whitespace-sig", "number-sig"} {
		if bytes.Contains(out, []byte(s)) {
			t.Fatalf("expected %q to be removed, got %s", s, string(out))
		}
	}
	if !bytes.Contains(out, []byte("keep")) {
		t.Fatalf("expected valid thinking block to remain, got %s", string(out))
	}
	if !bytes.Contains(out, []byte(`"hello"`)) {
		t.Fatalf("expected text block to remain, got %s", string(out))
	}
}

func TestSanitizeMessagesThinking_StripsSignatureFromToolUse(t *testing.T) {
	input := []byte(`{"messages":[{"role":"assistant","content":[` +
		`{"type":"thinking","thinking":"thought","signature":"valid-sig"},` +
		`{"type":"tool_use","id":"toolu_01","name":"Bash","input":{"cmd":"ls"},"signature":""}` +
		`]}]}`)

	out := SanitizeMessagesThinking(input)

	if bytes.Contains(out, []byte(`"signature":""`)) {
		t.Fatalf("expected empty signature on tool_use to be stripped, got %s", string(out))
	}
	if !bytes.Contains(out, []byte(`"valid-sig"`)) {
		t.Fatalf("expected valid thinking signature to remain, got %s", string(out))
	}
	if !bytes.Contains(out, []byte(`"tool_use"`)) {
		t.Fatalf("expected tool_use block to remain, got %s", string(out))
	}
}

func TestSanitizeMessagesThinking_LeavesUserMessagesAlone(t *testing.T) {
	// User messages should not be touched even if they contain malformed-looking blocks.
	input := []byte(`{"messages":[{"role":"user","content":[{"type":"text","text":"hi"}]}]}`)
	out := SanitizeMessagesThinking(input)
	if !bytes.Equal(out, input) {
		t.Fatalf("user message was modified:\nin=%s\nout=%s", input, out)
	}
}

func TestSanitizeMessagesThinking_NoOpOnInvalidInput(t *testing.T) {
	cases := [][]byte{
		nil,
		[]byte(""),
		[]byte("not json"),
		[]byte(`{"other":"field"}`),                // no messages array
		[]byte(`{"messages":{}}`),                  // messages not array
		[]byte(`{"messages":[{"role":"assistant"}]}`), // missing content
	}
	for _, in := range cases {
		out := SanitizeMessagesThinking(in)
		if !bytes.Equal(out, in) {
			t.Fatalf("expected no-op for %q, got %q", in, out)
		}
	}
}

func TestSanitizeMessagesThinking_StripsFernetSignature(t *testing.T) {
	// Codex/GPT executors emit `encrypted_content` (Fernet format, always
	// prefixed with "gAAAAAB") which the codex→claude response translator
	// writes into Claude `signature`. Replaying that to Anthropic triggers
	// "Invalid `signature` in `thinking` block". Sanitize must drop it.
	fernetSig := "gAAAAABp6DXjOFQ9Gjihsh7cy8RN4jrgdf_2HFRnc-1-vVtDBSV5b2NE47ZD94PVg1dfRuB2B0eCiMVSp7qozJUFvSkXI1gsIjSvLq5ExJJADPlJZsDSd6oUHatZxfEDJyn4U4r6JGDw"
	input := []byte(`{"messages":[{"role":"assistant","content":[` +
		`{"type":"thinking","thinking":"drop-fernet","signature":"` + fernetSig + `"},` +
		`{"type":"text","text":"keep-text"}` +
		`]}]}`)

	out := SanitizeMessagesThinking(input)

	if bytes.Contains(out, []byte("drop-fernet")) {
		t.Fatalf("expected Fernet-signed thinking block to be removed, got %s", string(out))
	}
	if bytes.Contains(out, []byte("gAAAAAB")) {
		t.Fatalf("expected Fernet signature to be gone, got %s", string(out))
	}
	if !bytes.Contains(out, []byte("keep-text")) {
		t.Fatalf("expected surrounding text block to remain, got %s", string(out))
	}
}

func TestSanitizeMessagesThinking_PreservesAnthropicLikeSignature(t *testing.T) {
	// Legitimate Anthropic thinking signatures are base64 HMAC-style strings
	// that never begin with "gAAAAAB". They must pass through untouched so
	// multi-turn extended thinking keeps working.
	anthropicSig := strings.Repeat("a", 200) // any non-Fernet long string
	input := []byte(`{"messages":[{"role":"assistant","content":[` +
		`{"type":"thinking","thinking":"keep-real","signature":"` + anthropicSig + `"},` +
		`{"type":"text","text":"ok"}` +
		`]}]}`)

	out := SanitizeMessagesThinking(input)

	if !bytes.Contains(out, []byte("keep-real")) {
		t.Fatalf("expected Anthropic thinking block to remain, got %s", string(out))
	}
	if !bytes.Contains(out, []byte(anthropicSig)) {
		t.Fatalf("expected Anthropic signature to remain, got %s", string(out))
	}
}

func TestSanitizeMessagesThinking_KimiLikePayloadIsCleaned(t *testing.T) {
	// Mirrors what Claude Code replays after a Kimi turn: assistant message with a
	// signature-less thinking block followed by text. Anthropic rejects this with
	// "Invalid `signature` in `thinking` block" — sanitize must drop the thinking block.
	input := []byte(`{"model":"claude-opus-4-7","messages":[` +
		`{"role":"user","content":[{"type":"text","text":"ping"}]},` +
		`{"role":"assistant","content":[` +
		`{"type":"thinking","thinking":"reasoning from kimi"},` +
		`{"type":"text","text":"pong"}` +
		`]},` +
		`{"role":"user","content":[{"type":"text","text":"continue"}]}` +
		`]}`)

	out := SanitizeMessagesThinking(input)

	if bytes.Contains(out, []byte("reasoning from kimi")) {
		t.Fatalf("expected kimi thinking block to be removed, got %s", string(out))
	}
	if !bytes.Contains(out, []byte(`"pong"`)) {
		t.Fatalf("expected assistant text to remain, got %s", string(out))
	}
	if !bytes.Contains(out, []byte(`"continue"`)) {
		t.Fatalf("expected trailing user message to remain, got %s", string(out))
	}
	if strings.Count(string(out), `"role":"assistant"`) != 1 {
		t.Fatalf("expected exactly one assistant message, got %s", string(out))
	}
}

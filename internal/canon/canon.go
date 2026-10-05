// Package canon produces the canonical JSON encoding that every hash in aios
// is computed over: proposal hashes (sha256 of the canonical instruction) and
// receipt hashes (sha256 of prev + the canonical record).
//
// Canonical form: object keys sorted by byte order, no insignificant
// whitespace, no HTML escaping, numbers written exactly as they appear in the
// first encoding (json.Number round trip, so 412.50 stays 412.50).
package canon

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// Marshal returns the canonical JSON encoding of v.
func Marshal(v any) ([]byte, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return Canonicalize(raw)
}

// Canonicalize re-encodes an existing JSON document in canonical form.
func Canonicalize(raw []byte) ([]byte, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var generic any
	if err := dec.Decode(&generic); err != nil {
		return nil, fmt.Errorf("canon: %w", err)
	}
	if dec.More() {
		return nil, fmt.Errorf("canon: trailing data after JSON value")
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	// encoding/json writes map keys in sorted order, which is what makes the
	// generic round trip canonical.
	if err := enc.Encode(generic); err != nil {
		return nil, fmt.Errorf("canon: %w", err)
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}

// SHA256Hex returns the lowercase hex sha256 of b.
func SHA256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// Hash returns SHA256Hex(Marshal(v)).
func Hash(v any) (string, error) {
	b, err := Marshal(v)
	if err != nil {
		return "", err
	}
	return SHA256Hex(b), nil
}

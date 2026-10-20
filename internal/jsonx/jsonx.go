// Package jsonx holds the two JSON helpers the masker and the stream rewriter
// share, so both treat numbers and escaping identically.
package jsonx

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
)

// Decode parses exactly one JSON value, keeping numbers as their original
// text so large integers and floats pass through byte for byte.
func Decode(b []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, errors.New("trailing data after JSON value")
	}
	return v, nil
}

// Encode writes v as compact JSON without HTML escaping, so "<" in a token or
// in user text is not turned into a Unicode escape.
func Encode(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

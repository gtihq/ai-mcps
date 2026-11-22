// Package omap implements a JSON object that preserves insertion order.
//
// Python dictionaries keep insertion order, and every MCP tool in this
// repository returns a dict. Go maps are marshalled with sorted keys, so a
// plain map[string]any would change the text that agents see. M keeps the
// Python ordering so the Go servers are byte-for-byte equivalent to the
// originals where it matters.
package omap

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// M is an ordered JSON object.
type M struct {
	keys []string
	vals map[string]any
}

// New returns an empty ordered object.
func New() *M {
	return &M{vals: make(map[string]any)}
}

// From returns an ordered object built from the key/value pairs in order.
// Each pair must be a two element slice: [key, value].
func From(pairs ...any) *M {
	m := New()
	for i := 0; i+1 < len(pairs); i += 2 {
		key, ok := pairs[i].(string)
		if !ok {
			panic(fmt.Sprintf("omap.From: pair %d key is %T, want string", i/2, pairs[i]))
		}
		m.Set(key, pairs[i+1])
	}
	return m
}

// Set adds or replaces a key, keeping its position when it already exists.
func (m *M) Set(key string, value any) *M {
	if m.vals == nil {
		m.vals = make(map[string]any)
	}
	if _, ok := m.vals[key]; !ok {
		m.keys = append(m.keys, key)
	}
	m.vals[key] = value
	return m
}

// SetIf sets a key only when value is not nil.
func (m *M) SetIf(key string, value any) *M {
	if value == nil {
		return m
	}
	return m.Set(key, value)
}

// Get returns the value for key.
func (m *M) Get(key string) (any, bool) {
	v, ok := m.vals[key]
	return v, ok
}

// Del removes a key.
func (m *M) Del(key string) *M {
	if _, ok := m.vals[key]; ok {
		delete(m.vals, key)
		for i, k := range m.keys {
			if k == key {
				m.keys = append(m.keys[:i], m.keys[i+1:]...)
				break
			}
		}
	}
	return m
}

// Keys returns the keys in insertion order.
func (m *M) Keys() []string {
	out := make([]string, len(m.keys))
	copy(out, m.keys)
	return out
}

// Len returns the number of keys.
func (m *M) Len() int { return len(m.keys) }

// MarshalJSON implements json.Marshaler.
func (m *M) MarshalJSON() ([]byte, error) {
	if m == nil {
		return []byte("null"), nil
	}
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, k := range m.keys {
		if i > 0 {
			buf.WriteByte(',')
		}
		kb, err := json.Marshal(k)
		if err != nil {
			return nil, err
		}
		buf.Write(kb)
		buf.WriteByte(':')
		vb, err := json.Marshal(m.vals[k])
		if err != nil {
			return nil, err
		}
		buf.Write(vb)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// UnmarshalJSON implements json.Unmarshaler and preserves key order.
func (m *M) UnmarshalJSON(data []byte) error {
	*m = *New()
	dec := json.NewDecoder(bytes.NewReader(data))
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return fmt.Errorf("omap: expected object, got %v", tok)
	}
	for dec.More() {
		kt, err := dec.Token()
		if err != nil {
			return err
		}
		key, ok := kt.(string)
		if !ok {
			return fmt.Errorf("omap: expected string key, got %v", kt)
		}
		var value any
		if err := dec.Decode(&value); err != nil {
			return err
		}
		m.Set(key, value)
	}
	_, err = dec.Token()
	return err
}

// String returns the compact JSON representation.
func (m *M) String() string {
	b, _ := json.Marshal(m)
	return string(b)
}

// AsMap returns a plain map, useful for code that only reads values.
func (m *M) AsMap() map[string]any {
	out := make(map[string]any, len(m.vals))
	for k, v := range m.vals {
		out[k] = v
	}
	return out
}

// Wrap marshals v into compact JSON and unmarshals it into an ordered object.
// Nested objects become plain maps; top-level order is preserved.
func Wrap(v any) *M {
	if m, ok := v.(*M); ok {
		return m
	}
	b, err := json.Marshal(v)
	if err != nil {
		return New()
	}
	m := New()
	if err := json.Unmarshal(b, m); err != nil {
		return New()
	}
	return m
}

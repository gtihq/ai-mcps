package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/gtihq/ai-mcps/internal/omap"
)

// ---- low level ordered JSON helpers -----------------------------------------

// decodeJSON decodes JSON into *omap.M objects, preserving insertion order,
// and converts json.Number to int64 / float64 exactly like Python's json.loads
// (which yields int for "12" and float for "1.5").
func decodeJSON(data []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	return decodeValue(dec)
}

func decodeValue(dec *json.Decoder) (any, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	switch v := tok.(type) {
	case json.Delim:
		switch v {
		case '{':
			m := omap.New()
			for dec.More() {
				keyTok, err := dec.Token()
				if err != nil {
					return nil, err
				}
				key, ok := keyTok.(string)
				if !ok {
					return nil, fmt.Errorf("omap: expected string key, got %v", keyTok)
				}
				value, err := decodeValue(dec)
				if err != nil {
					return nil, err
				}
				m.Set(key, value)
			}
			_, err = dec.Token()
			return m, err
		case '[':
			values := make([]any, 0)
			for dec.More() {
				value, err := decodeValue(dec)
				if err != nil {
					return nil, err
				}
				values = append(values, value)
			}
			_, err = dec.Token()
			return values, err
		default:
			return nil, fmt.Errorf("unexpected delimiter %v", v)
		}
	case json.Number:
		s := string(v)
		if strings.ContainsAny(s, ".eE") {
			f, err := v.Float64()
			if err != nil {
				return nil, err
			}
			return f, nil
		}
		if i, err := v.Int64(); err == nil {
			return i, nil
		}
		f, err := v.Float64()
		if err != nil {
			return nil, err
		}
		return f, nil
	default:
		return tok, nil
	}
}

func asM(v any) *omap.M {
	switch x := v.(type) {
	case *omap.M:
		return x
	case map[string]any:
		return omap.Wrap(x)
	default:
		return nil
	}
}

func asSlice(v any) []any {
	if s, ok := v.([]any); ok {
		return s
	}
	return nil
}

func mget(m *omap.M, key string) any {
	if m == nil {
		return nil
	}
	v, _ := m.Get(key)
	return v
}

// first is the helper used in mcp_server._parse_search_items and in parsers:
// the first value that is not None and not "".
func first(vals ...any) any {
	for _, v := range vals {
		if v == nil {
			continue
		}
		if s, ok := v.(string); ok && s == "" {
			continue
		}
		return v
	}
	return nil
}

// firstNN is parse_shop's local first(): the first value that is not None.
func firstNN(vals ...any) any {
	for _, v := range vals {
		if v != nil {
			return v
		}
	}
	return nil
}

// firstTruthy emulates Python's `a or b or c`.
func firstTruthy(vals ...any) any {
	for _, v := range vals {
		if isTruthy(v) {
			return v
		}
	}
	return nil
}

func isTruthy(v any) bool {
	if v == nil {
		return false
	}
	switch x := v.(type) {
	case bool:
		return x
	case string:
		return x != ""
	case int:
		return x != 0
	case int64:
		return x != 0
	case float64:
		return x != 0
	case json.Number:
		f, err := x.Float64()
		return err == nil && f != 0
	case []any:
		return len(x) > 0
	case *omap.M:
		return x.Len() > 0
	case map[string]any:
		return len(x) > 0
	}
	return true
}

func pyBool(v any) bool { return isTruthy(v) }

func toFloat(v any) (float64, bool) {
	switch x := v.(type) {
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	case float64:
		return x, true
	case json.Number:
		f, err := x.Float64()
		return f, err == nil
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(x), 64)
		return f, err == nil
	}
	return 0, false
}

func toInt64(v any) (int64, bool) {
	switch x := v.(type) {
	case int:
		return int64(x), true
	case int64:
		return x, true
	case float64:
		return int64(x), true
	case json.Number:
		i, err := x.Int64()
		if err == nil {
			return i, true
		}
		f, err := x.Float64()
		return int64(f), err == nil
	case string:
		i, err := strconv.ParseInt(strings.TrimSpace(x), 10, 64)
		if err == nil {
			return i, true
		}
		f, err := strconv.ParseFloat(strings.TrimSpace(x), 64)
		return int64(f), err == nil
	}
	return 0, false
}

func round2(f float64) float64 { return math.Round(f*100) / 100 }

func numEq(a, b any) bool {
	fa, oka := toFloat(a)
	fb, okb := toFloat(b)
	return oka && okb && fa == fb
}

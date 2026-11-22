package device

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// sortedKeys returns map keys in deterministic order.
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// MapString converts an arbitrary decoded JSON value to a trimmed string.
// JSON numbers keep their original spelling when UseNumber is enabled.
func MapString(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(x)
	case json.Number:
		return x.String()
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case float32:
		return strconv.FormatFloat(float64(x), 'f', -1, 32)
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case bool:
		return strconv.FormatBool(x)
	default:
		return strings.TrimSpace(fmt.Sprint(x))
	}
}

// MapBool interprets a decoded value as a boolean.
func MapBool(v any) bool {
	switch x := v.(type) {
	case bool:
		return x
	case string:
		return BoolText(x)
	case float64:
		return x != 0
	case json.Number:
		f, err := x.Float64()
		return err == nil && f != 0
	default:
		return false
	}
}

// MapAny converts a decoded value into a string keyed map.
func MapAny(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return nil
}

// MapSlice converts a decoded value into a slice.
func MapSlice(v any) []any {
	if s, ok := v.([]any); ok {
		return s
	}
	return nil
}

// First returns the first non-nil value among the supplied keys.
func First(m map[string]any, keys ...string) any {
	if m == nil {
		return nil
	}
	for _, k := range keys {
		if v, ok := m[k]; ok && v != nil {
			return v
		}
	}
	return nil
}

// Str returns the first non-empty string among the supplied keys.
func Str(m map[string]any, keys ...string) string {
	for _, v := range keys {
		if s := MapString(m[v]); s != "" {
			return s
		}
	}
	return ""
}

// Extra copies a decoded object into a plain map, dropping the listed keys.
func Extra(m map[string]any, drop ...string) map[string]any {
	if m == nil {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		skip := false
		for _, d := range drop {
			if k == d {
				skip = true
				break
			}
		}
		if !skip {
			out[k] = v
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

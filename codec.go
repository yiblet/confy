package confy

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// Codec is the leaf-level parse. Name is the type name shown in docs;
// Decode turns a scalar into an A (it should accept the string form too, so
// text-only sources such as flags work); Encode renders an A so that Default
// values can appear in Docs and Template.
type Codec[A any] struct {
	Name   string
	Decode func(any) (A, error)
	Encode func(A) string
}

// StringCodec accepts any scalar and stringifies it.
var StringCodec = Codec[string]{
	Name: "string",
	Decode: func(v any) (string, error) {
		switch x := v.(type) {
		case string:
			return x, nil
		case json.Number:
			return x.String(), nil
		default:
			return fmt.Sprint(v), nil
		}
	},
	Encode: func(s string) string { return s },
}

// IntCodec accepts Go integer types, whole floats, json.Number, and
// decimal strings.
var IntCodec = Codec[int]{
	Name: "int",
	Decode: func(v any) (int, error) {
		n, err := toInt64(v)
		if err != nil {
			return 0, err
		}
		if int64(int(n)) != n {
			return 0, fmt.Errorf("out of range for int")
		}
		return int(n), nil
	},
	Encode: strconv.Itoa,
}

// Int64Codec is IntCodec at 64 bits.
var Int64Codec = Codec[int64]{
	Name:   "int64",
	Decode: toInt64,
	Encode: func(n int64) string { return strconv.FormatInt(n, 10) },
}

// FloatCodec accepts numbers and numeric strings.
var FloatCodec = Codec[float64]{
	Name: "float",
	Decode: func(v any) (float64, error) {
		switch x := v.(type) {
		case float64:
			return x, nil
		case float32:
			return float64(x), nil
		case int:
			return float64(x), nil
		case int64:
			return float64(x), nil
		case uint64:
			return float64(x), nil
		case json.Number:
			return x.Float64()
		case string:
			return strconv.ParseFloat(strings.TrimSpace(x), 64)
		}
		return 0, fmt.Errorf("expected float, got %T", v)
	},
	Encode: func(f float64) string { return strconv.FormatFloat(f, 'g', -1, 64) },
}

// BoolCodec accepts bools and the strings strconv.ParseBool accepts.
var BoolCodec = Codec[bool]{
	Name: "bool",
	Decode: func(v any) (bool, error) {
		switch x := v.(type) {
		case bool:
			return x, nil
		case string:
			return strconv.ParseBool(strings.TrimSpace(x))
		}
		return false, fmt.Errorf("expected bool, got %T", v)
	},
	Encode: strconv.FormatBool,
}

// DurationCodec accepts time.ParseDuration strings and integer nanoseconds.
var DurationCodec = Codec[time.Duration]{
	Name: "duration",
	Decode: func(v any) (time.Duration, error) {
		switch x := v.(type) {
		case string:
			return time.ParseDuration(strings.TrimSpace(x))
		case time.Duration:
			return x, nil
		}
		n, err := toInt64(v)
		if err != nil {
			return 0, fmt.Errorf("expected duration, got %T", v)
		}
		return time.Duration(n), nil
	},
	Encode: time.Duration.String,
}

// Enum is a string codec restricted to the given values; the docs name is
// "one of: a, b, c".
func Enum(values ...string) Codec[string] {
	return Codec[string]{
		Name: "one of: " + strings.Join(values, ", "),
		Decode: func(v any) (string, error) {
			s, err := StringCodec.Decode(v)
			if err != nil {
				return "", err
			}
			for _, w := range values {
				if w == s {
					return s, nil
				}
			}
			return "", fmt.Errorf("must be one of: %s", strings.Join(values, ", "))
		},
		Encode: func(s string) string { return s },
	}
}

func toInt64(v any) (int64, error) {
	switch x := v.(type) {
	case int:
		return int64(x), nil
	case int8:
		return int64(x), nil
	case int16:
		return int64(x), nil
	case int32:
		return int64(x), nil
	case int64:
		return x, nil
	case uint:
		return int64(x), nil
	case uint8:
		return int64(x), nil
	case uint16:
		return int64(x), nil
	case uint32:
		return int64(x), nil
	case uint64:
		if x > math.MaxInt64 {
			return 0, fmt.Errorf("out of range")
		}
		return int64(x), nil
	case float64:
		if x != math.Trunc(x) {
			return 0, fmt.Errorf("expected integer, got %v", x)
		}
		return int64(x), nil
	case float32:
		return toInt64(float64(x))
	case json.Number:
		return x.Int64()
	case string:
		return strconv.ParseInt(strings.TrimSpace(x), 10, 64)
	}
	return 0, fmt.Errorf("expected integer, got %T", v)
}

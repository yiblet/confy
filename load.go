package confy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
)

// Load decodes data with unmarshal and then Builds with opts. Pass the
// Unmarshal function of whatever format library you already use:
// toml.Unmarshal, yaml.Unmarshal, json.Unmarshal. To layer several sources,
// decode into an `any` yourself, wrap it with FromAny, and call Build.
func (c Confy[A]) Load(data []byte, unmarshal func([]byte, any) error, opts ...BuildOption) (A, error) {
	var raw any
	if err := unmarshal(data, &raw); err != nil {
		var z A
		return z, fmt.Errorf("confy: %w", err)
	}
	return c.Build(FromAny(raw), opts...)
}

// Decode reads one document from r with a streaming decoder and then
// Builds. newDecoder is the constructor of any decoder with a
// Decode(any) error method: json.NewDecoder, toml.NewDecoder,
// yaml.NewDecoder.
func (c Confy[A]) Decode[D interface{ Decode(any) error }](r io.Reader, newDecoder func(io.Reader) D, opts ...BuildOption) (A, error) {
	var raw any
	if err := newDecoder(r).Decode(&raw); err != nil {
		var z A
		return z, fmt.Errorf("confy: %w", err)
	}
	return c.Build(FromAny(raw), opts...)
}

// LoadJSON decodes JSON with the standard library and then Builds. Numbers are kept as json.Number so integers beyond 2^53
// survive; the built-in codecs accept json.Number.
func (c Confy[A]) LoadJSON(data []byte, opts ...BuildOption) (A, error) {
	return c.Decode(bytes.NewReader(data), jsonDecoder, opts...)
}

// LoadJSONFile reads and decodes a JSON file and then Builds. Errors name
// the path.
func (c Confy[A]) LoadJSONFile(path string, opts ...BuildOption) (A, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		var z A
		return z, fmt.Errorf("confy: %w", err)
	}
	a, err := c.LoadJSON(data, opts...)
	if err != nil {
		return a, fmt.Errorf("%s: %w", path, err)
	}
	return a, nil
}

func jsonDecoder(r io.Reader) *json.Decoder {
	d := json.NewDecoder(r)
	d.UseNumber()
	return d
}

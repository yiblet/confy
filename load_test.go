package confy

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type hostCfg struct{ Host string }

func hostReader() Confy[hostCfg] {
	return Struct[hostCfg]().At(func(s *hostCfg) *string { return &s.Host }, String("host"))
}

func TestLoadWithStdlibJSON(t *testing.T) {
	got, err := hostReader().Load([]byte(`{"host":"h"}`), json.Unmarshal)
	if err != nil || got.Host != "h" {
		t.Fatalf("got %+v err %v", got, err)
	}
	if _, err := hostReader().Load([]byte(`{`), json.Unmarshal); err == nil || !strings.HasPrefix(err.Error(), "confy: ") {
		t.Fatalf("expected wrapped decode error, got %v", err)
	}
	// Strict: an unused key is an error.
	if _, err := hostReader().Load([]byte(`{"host":"h","hots":1}`), json.Unmarshal); err == nil || !strings.Contains(err.Error(), "hots: unused key") {
		t.Fatalf("expected strict error, got %v", err)
	}
}

// fakeDecoder stands in for toml.NewDecoder / yaml.NewDecoder: a pointer
// type with a Decode(any) error method, constructed from an io.Reader.
type fakeDecoder struct{ r io.Reader }

func newFakeDecoder(r io.Reader) *fakeDecoder { return &fakeDecoder{r} }
func (d *fakeDecoder) Decode(v any) error {
	b, err := io.ReadAll(d.r)
	if err != nil {
		return err
	}
	*(v.(*any)) = map[string]any{"host": strings.TrimSpace(string(b))}
	return nil
}

func TestDecodeInfersDecoderType(t *testing.T) {
	got, err := hostReader().Decode(strings.NewReader("hello\n"), newFakeDecoder)
	if err != nil || got.Host != "hello" {
		t.Fatalf("got %+v err %v", got, err)
	}
	got, err = hostReader().Decode(strings.NewReader(`{"host": "d"}`), json.NewDecoder)
	if err != nil || got.Host != "d" {
		t.Fatalf("got %+v err %v", got, err)
	}
}

func TestLoadJSONKeepsBigIntegers(t *testing.T) {
	n, err := Int64("n").LoadJSON([]byte(`{"n": 9007199254740993}`))
	if err != nil || n != 9007199254740993 {
		t.Fatalf("got %d err %v", n, err)
	}
}

func TestLoadJSONFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "app.json")
	if err := os.WriteFile(p, []byte(`{"host": "h"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := hostReader().LoadJSONFile(p)
	if err != nil || got.Host != "h" {
		t.Fatalf("got %+v err %v", got, err)
	}
	if _, err := hostReader().LoadJSONFile(filepath.Join(dir, "missing.json")); err == nil {
		t.Fatal("expected error for missing file")
	}
	if err := os.WriteFile(p, []byte(`{`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := hostReader().LoadJSONFile(p); err == nil || !strings.Contains(err.Error(), "app.json") {
		t.Fatalf("error should name the file, got %v", err)
	}
}

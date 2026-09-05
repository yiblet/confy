package example_test

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/netip"
	"net/url"
	"strings"

	"github.com/yiblet/confy"
)

// Custom codecs: the leaf-level parse carries its own type name (for docs)
// and an encoder (so defaults can be rendered).

var URLCodec = confy.Codec[*url.URL]{
	Name: "url",
	Decode: func(v any) (*url.URL, error) {
		s, err := confy.StringCodec.Decode(v)
		if err != nil {
			return nil, err
		}
		u, err := url.Parse(s)
		if err != nil {
			return nil, err
		}
		if u.Scheme == "" || u.Host == "" {
			return nil, fmt.Errorf("need scheme and host")
		}
		return u, nil
	},
	Encode: (*url.URL).String,
}

var AddrPortCodec = confy.Codec[netip.AddrPort]{
	Name: "ip:port",
	Decode: func(v any) (netip.AddrPort, error) {
		s, _ := confy.StringCodec.Decode(v)
		return netip.ParseAddrPort(s)
	},
	Encode: netip.AddrPort.String,
}

var LevelCodec = confy.Codec[slog.Level]{
	Name: "one of: DEBUG, INFO, WARN, ERROR",
	Decode: func(v any) (slog.Level, error) {
		s, _ := confy.StringCodec.Decode(v)
		var l slog.Level
		return l, l.UnmarshalText([]byte(strings.ToUpper(s)))
	},
	Encode: slog.Level.String,
}

// Bytes accepts "512", "10k", "2M", "1G".
var BytesCodec = confy.Codec[int64]{
	Name: "bytes",
	Decode: func(v any) (int64, error) {
		s, _ := confy.StringCodec.Decode(v)
		s = strings.TrimSpace(strings.ToLower(s))
		mult := int64(1)
		for suffix, m := range map[string]int64{"k": 1 << 10, "m": 1 << 20, "g": 1 << 30} {
			if strings.HasSuffix(s, suffix) {
				mult, s = m, strings.TrimSuffix(s, suffix)
				break
			}
		}
		n, err := confy.Int64Codec.Decode(s)
		if err != nil {
			return 0, fmt.Errorf("bad size %q", v)
		}
		return n * mult, nil
	},
	Encode: func(n int64) string {
		switch {
		case n%(1<<30) == 0:
			return fmt.Sprintf("%dG", n>>30)
		case n%(1<<20) == 0:
			return fmt.Sprintf("%dM", n>>20)
		case n%(1<<10) == 0:
			return fmt.Sprintf("%dk", n>>10)
		}
		return fmt.Sprint(n)
	},
}

type Limits struct {
	Endpoint *url.URL
	Bind     netip.AddrPort
	Level    slog.Level
	MaxBody  int64
	MaxFiles int
}

func limitsConfy() confy.Confy[Limits] {
	return confy.Struct[Limits]().
		At(func(l *Limits) **url.URL { return &l.Endpoint }, confy.Field("endpoint", URLCodec).Doc("Upstream API")).
		At(func(l *Limits) *netip.AddrPort { return &l.Bind }, confy.Field("bind", AddrPortCodec).Default(netip.MustParseAddrPort("127.0.0.1:8080"))).
		At(func(l *Limits) *slog.Level { return &l.Level }, confy.Field("log_level", LevelCodec).Default(slog.LevelInfo)).
		At(func(l *Limits) *int64 { return &l.MaxBody }, confy.Field("max_body", BytesCodec).Default(4<<20)).
		At(func(l *Limits) *int { return &l.MaxFiles }, confy.Int("max_files").Default(64).Check(func(n int) error {
			if n <= 0 {
				return fmt.Errorf("must be positive")
			}
			return nil
		}))
}

func Example_customCodecs() {
	l, err := limitsConfy().Build(parse(`{"endpoint": "https://api.example.com/v1", "log_level": "warn", "max_body": "1G"}`))
	fmt.Println("err:", err)
	fmt.Println(l.Endpoint.Host, l.Bind, l.Level, l.MaxBody, l.MaxFiles)

	_, err = limitsConfy().Build(parse(`{"endpoint": "example.com", "bind": "localhost:80", "log_level": "loud", "max_body": "1TB", "max_files": 0}`))
	fmt.Println(err)

	// Output:
	// err: <nil>
	// api.example.com 127.0.0.1:8080 WARN 1073741824 64
	// endpoint: expected url, got "example.com": need scheme and host
	// bind: expected ip:port, got "localhost:80": ParseAddr("localhost"): unable to parse IP
	// log_level: expected one of: DEBUG, INFO, WARN, ERROR, got "loud": slog: level string "LOUD": unknown name
	// max_body: expected bytes, got "1TB": bad size "1TB"
	// max_files: must be positive
}

func Example_customCodecDefaultsRender() {
	// The encoder half of each codec is what lets Template and Docs print
	// real defaults instead of blanks, and JSON Schema map the type.
	s := limitsConfy().Schema()
	fmt.Print(confy.Template(s))
	fmt.Println("---")
	js, _ := json.MarshalIndent(confy.JSONSchema(s)["properties"].(map[string]any)["max_body"], "", "  ")
	fmt.Println(string(js))

	// Output:
	// # Upstream API
	// endpoint: <url>
	// bind: 127.0.0.1:8080
	// log_level: INFO
	// max_body: 4M
	// max_files: 64
	// ---
	// {
	//   "default": "4M",
	//   "format": "bytes",
	//   "type": "string"
	// }
}

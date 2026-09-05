package confy_test

import (
	"encoding/json"
	"fmt"

	"github.com/yiblet/confy"
)

type TLS interface{ isTLS() }
type Off struct{}
type Acme struct{ Email string }
type Manual struct{ Cert, Key string }

func (Off) isTLS()    {}
func (Acme) isTLS()   {}
func (Manual) isTLS() {}

type Server struct {
	Host      string
	Port      int
	TLS       TLS
	Upstreams []string
}

// The reader is the description: one value, four interpreters.
func Example() {
	tls := confy.Switch("mode",
		confy.Case("off", confy.Pure(Off{}), func(o Off) TLS { return o }),
		confy.Case("acme",
			confy.Struct[Acme]().
				At(func(a *Acme) *string { return &a.Email }, confy.String("email")),
			func(a Acme) TLS { return a }),
		confy.Case("manual",
			confy.Struct[Manual]().
				At(func(m *Manual) *string { return &m.Cert }, confy.String("cert")).
				At(func(m *Manual) *string { return &m.Key }, confy.String("key").Secret()),
			func(m Manual) TLS { return m }),
	)

	server := confy.Struct[Server]().
		At(func(s *Server) *string { return &s.Host }, confy.String("host").Doc("Listen address")).
		At(func(s *Server) *int { return &s.Port }, confy.Int("port").Default(8080)).
		At(func(s *Server) *TLS { return &s.TLS }, confy.Under("tls", tls)).
		At(func(s *Server) *[]string { return &s.Upstreams }, confy.List("upstreams", confy.String("url")))

	var input any
	_ = json.Unmarshal([]byte(`{
		"host": "example.com",
		"tls": {"mode": "acme", "email": "ops@example.com"},
		"upstreams": [{"url": "http://a"}, {"url": "http://b"}],
		"prot": 1
	}`), &input)
	v := confy.FromAny(input)

	srv, err := server.Build(v)
	fmt.Printf("run:     %+v (err=%v)\n", srv, err)
	fmt.Printf("unused: %v\n", server.Unused(v))

	_, err = server.Build(confy.FromAny(map[string]any{"port": "x", "tls": map[string]any{"mode": "manual"}}))
	fmt.Println("errors, all at once:")
	fmt.Println(err)

	fmt.Println("docs:")
	fmt.Print(confy.Docs(server.Schema()))
	fmt.Println("template:")
	fmt.Print(confy.Template(server.Schema()))

	// Output:
	// run:     {Host:example.com Port:8080 TLS:{Email:ops@example.com} Upstreams:[http://a http://b]} (err=prot: unused key)
	// unused: [prot]
	// errors, all at once:
	// host: missing
	// port: expected int, got "x": strconv.ParseInt: parsing "x": invalid syntax
	// tls.cert: missing
	// tls.key: missing
	// upstreams: missing
	// docs:
	// host             string                     required
	//     Listen address
	// port             int                        default 8080
	// tls.mode         one of: off, acme, manual  required
	// tls.email        string                     required  when tls.mode=acme
	// tls.cert         string                     required  when tls.mode=manual
	// tls.key          string                     required  when tls.mode=manual  [secret]
	// upstreams[].url  string                     required
	// template:
	// # Listen address
	// host: <string>
	// port: 8080
	// tls:
	//   mode: <off|acme|manual>
	//   # when mode = off: (no further keys)
	//   # when mode = acme:
	//   email: <string>
	//   # when mode = manual:
	//   cert: <string>
	//   key: <secret>
	// upstreams:
	//   - url: <string>
}

package confy

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

// --- worked example: a server with a TLS sum type -------------------------------------------------

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
	Timeout   time.Duration
}

func tlsConfy() Confy[TLS] {
	return Switch("mode",
		Case("off", Pure(Off{}), func(o Off) TLS { return o }),
		Case("acme", Struct[Acme]().
			At(func(a *Acme) *string { return &a.Email }, String("email").Doc("ACME account email")),
			func(a Acme) TLS { return a }),
		Case("manual", Struct[Manual]().
			At(func(m *Manual) *string { return &m.Cert }, String("cert")).
			At(func(m *Manual) *string { return &m.Key }, String("key").Secret()),
			func(m Manual) TLS { return m }),
	)
}

func serverConfy() Confy[Server] {
	return Struct[Server]().
		At(func(s *Server) *string { return &s.Host }, String("host").Doc("Listen address")).
		At(func(s *Server) *int { return &s.Port }, Int("port").Default(8080).Check(func(p int) error {
			if p <= 0 || p > 65535 {
				return fmt.Errorf("port %d out of range", p)
			}
			return nil
		})).
		At(func(s *Server) *TLS { return &s.TLS }, Under("tls", tlsConfy())).
		At(func(s *Server) *[]string { return &s.Upstreams }, List("upstreams", String("url"))).
		At(func(s *Server) *time.Duration { return &s.Timeout }, Duration("timeout").Default(30*time.Second))
}

func mustJSON(t *testing.T, s string) Value {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		t.Fatal(err)
	}
	return FromAny(v)
}

func TestExampleRunOK(t *testing.T) {
	v := mustJSON(t, `{"host":"example.com","tls":{"mode":"acme","email":"ops@example.com"},
	                   "upstreams":[{"url":"http://a"},{"url":"http://b"}]}`)
	got, err := serverConfy().Build(v)
	if err != nil {
		t.Fatal(err)
	}
	want := Server{Host: "example.com", Port: 8080, TLS: Acme{"ops@example.com"}, Upstreams: []string{"http://a", "http://b"}, Timeout: 30 * time.Second}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v want %+v", got, want)
	}
}

func TestExampleAllErrorsAtOnce(t *testing.T) {
	v := mustJSON(t, `{"port":"abc","tls":{"mode":"manual","key":123},"upstreams":[{"url":"x"},{}]}`)
	_, err := serverConfy().Build(v)
	if err == nil {
		t.Fatal("expected issues")
	}
	var issues Issues
	if !errors.As(err, &issues) {
		t.Fatalf("error is %T", err)
	}
	paths := make([]string, len(issues))
	for i, iss := range issues {
		paths[i] = iss.Path.String()
	}
	want := []string{"host", "port", "tls.cert", "upstreams[1].url"}
	if !reflect.DeepEqual(paths, want) {
		t.Fatalf("issue paths %v, want %v\n%v", paths, want, err)
	}
	// errors.As sees through Issues via Unwrap() []error.
	var one Issue
	if !errors.As(err, &one) || one.Path.String() != "host" {
		t.Fatalf("errors.As should find the first issue, got %+v", one)
	}
}

func TestSwitchTagErrors(t *testing.T) {
	_, err := serverConfy().Build(mustJSON(t, `{"host":"h","tls":{},"upstreams":[]}`))
	if err == nil || !strings.Contains(err.Error(), "tls.mode: missing (one of: off, acme, manual)") {
		t.Fatalf("got %v", err)
	}
	_, err = serverConfy().Build(mustJSON(t, `{"host":"h","tls":{"mode":"tls13"},"upstreams":[]}`))
	if err == nil || !strings.Contains(err.Error(), `tls.mode: unknown tag "tls13"`) {
		t.Fatalf("got %v", err)
	}
}

func TestUnusedUsesUnionOfArms(t *testing.T) {
	c := serverConfy()
	// Tag selects "acme": "cert" is unused here, "email" is claimed, "typo" is unused.
	v := mustJSON(t, `{"host":"h","typo":1,"tls":{"mode":"acme","email":"e","cert":"c"},"upstreams":[{"url":"u","extra":true}]}`)
	got := pathStrings(c.Unused(v))
	want := []string{"tls.cert", "typo", "upstreams[0].extra"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unused = %v, want %v", got, want)
	}
	// No tag: every arm's keys are claimed (sound over-approximation).
	v = mustJSON(t, `{"host":"h","tls":{"email":"e","cert":"c","bogus":1},"upstreams":[]}`)
	got = pathStrings(c.Unused(v))
	if want := []string{"tls.bogus"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("unused = %v, want %v", got, want)
	}
	// Build folds unused keys into the issues.
	_, err := c.Build(mustJSON(t, `{"host":"h","tls":{"mode":"off"},"upstreams":[],"prot":1}`))
	if err == nil || !strings.Contains(err.Error(), "prot: unused key") {
		t.Fatalf("got %v", err)
	}
}

func pathStrings(ps []Path) []string {
	out := make([]string, len(ps))
	for i, p := range ps {
		out[i] = p.String()
	}
	return out
}

func TestDocs(t *testing.T) {
	d := Docs(serverConfy().Schema())
	for _, want := range []string{
		"host", "string", "required", "Listen address",
		"port", "default 8080",
		"tls.mode", "one of: off, acme, manual",
		"tls.email", "when tls.mode=acme", "ACME account email",
		"tls.key", "when tls.mode=manual", "[secret]",
		"upstreams[].url",
		"timeout", "duration", "default 30s",
	} {
		if !strings.Contains(d, want) {
			t.Errorf("docs missing %q:\n%s", want, d)
		}
	}
}

func TestTemplate(t *testing.T) {
	tpl := Template(serverConfy().Schema())
	for _, want := range []string{
		"# Listen address\nhost: <string>",
		"port: 8080",
		"tls:\n  mode: <off|acme|manual>",
		"# when mode = off: (no further keys)",
		"# when mode = acme:",
		"  # ACME account email\n  email: <string>",
		"key: <secret>",
		"upstreams:\n  - url: <string>",
		"timeout: 30s",
	} {
		if !strings.Contains(tpl, want) {
			t.Errorf("template missing %q:\n%s", want, tpl)
		}
	}
}

func TestJSONSchema(t *testing.T) {
	js := JSONSchema(serverConfy().Schema())
	b, _ := json.Marshal(js)
	s := string(b)
	for _, want := range []string{
		`"unevaluatedProperties":false`,
		`"required":["host","tls","upstreams"]`,
		`"default":"8080"`,
		`"const":"acme"`,
		`"discriminator":{"propertyName":"mode"}`,
		`"writeOnly":true`,
		`"items":{"properties":{"url":{"type":"string"}}`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("json schema missing %s:\n%s", want, s)
		}
	}
	if strings.Contains(s, "additionalProperties\":false") {
		t.Error("must not use additionalProperties:false")
	}
}

func TestLayer(t *testing.T) {
	file := mustJSON(t, `{"host":"file","port":1,"tls":{"mode":"off"},"upstreams":[]}`)
	env := mustJSON(t, `{"port":2,"tls":{"mode":"acme","email":"e"}}`)
	got, err := serverConfy().Build(Layer(file, env))
	if err != nil {
		t.Fatal(err)
	}
	if got.Host != "file" || got.Port != 2 || !reflect.DeepEqual(got.TLS, Acme{"e"}) {
		t.Fatalf("got %+v", got)
	}
}

// --- other combinators ------------------------------------------------------

func TestOptionalAndDefault(t *testing.T) {
	c := Struct[struct {
		A *int
		B *Acme
		C string
	}]().
		At(func(s *struct {
			A *int
			B *Acme
			C string
		}) **int {
			return &s.A
		}, Optional(Int("a"))).
		At(func(s *struct {
			A *int
			B *Acme
			C string
		}) **Acme {
			return &s.B
		}, Optional(Under("b", Struct[Acme]().At(func(a *Acme) *string { return &a.Email }, String("email"))))).
		At(func(s *struct {
			A *int
			B *Acme
			C string
		}) *string {
			return &s.C
		}, String("c").Default("dflt"))
	got, err := c.Build(mustJSON(t, `{}`))
	if err != nil {
		t.Fatal(err)
	}
	if got.A != nil || got.B != nil || got.C != "dflt" {
		t.Fatalf("got %+v", got)
	}
	got, err = c.Build(mustJSON(t, `{"a":3,"b":{"email":"e"},"c":"x"}`))
	if err != nil || *got.A != 3 || got.B.Email != "e" || got.C != "x" {
		t.Fatalf("got %+v err %v", got, err)
	}
	// Present but wrong: still an error (optional is about absence only).
	if _, err := c.Build(mustJSON(t, `{"a":"nope"}`)); err == nil {
		t.Fatal("expected type error for present-but-invalid optional")
	}
	// Schema shows optionality and the default.
	s := c.Schema().Root.(Object)
	if !s.Fields[0].Optional || !s.Fields[1].Optional {
		t.Fatalf("schema should mark a and b optional: %+v", s)
	}
	if l := s.Fields[2].Node.(Leaf); l.Default == nil || *l.Default != "dflt" {
		t.Fatalf("default not in schema: %+v", l)
	}
}

func TestDefaultPanicsAfterMap(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic")
		}
	}()
	Int("a").Map(func(i int) int { return i }).Default(1)
}

func TestConflictPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil || !strings.Contains(fmt.Sprint(r), "confy:") {
			t.Fatalf("expected construction panic, got %v", r)
		}
	}()
	String("a").With(List("a", String("x")), func(string, []string) int { return 0 })
}

func TestShapeShortOrLong(t *testing.T) {
	type Port struct {
		Target    int
		Published string
	}
	// compose-style: "8080:80" or {target: 80, published: "8080"}
	port := Shape(
		KindCase(KindScalar, Scalar(StringCodec), func(s string) Port {
			pub, tgt, _ := strings.Cut(s, ":")
			var p Port
			fmt.Sscan(tgt, &p.Target)
			p.Published = pub
			return p
		}),
		KindArm(KindMapping, Struct[Port]().
			At(func(p *Port) *int { return &p.Target }, Int("target")).
			At(func(p *Port) *string { return &p.Published }, String("published"))),
	)
	ports := List("ports", port)
	got, err := ports.Build(mustJSON(t, `{"ports":["8080:80",{"target":443,"published":"8443"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if want := []Port{{80, "8080"}, {443, "8443"}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v", got)
	}
	_, err = ports.Build(mustJSON(t, `{"ports":[[1]]}`))
	if err == nil || !strings.Contains(err.Error(), "ports[0]: expected scalar or mapping, got sequence") {
		t.Fatalf("got %v", err)
	}
	if u := pathStrings(ports.Unused(mustJSON(t, `{"ports":[{"target":1,"published":"p","x":1}]}`))); !reflect.DeepEqual(u, []string{"ports[0].x"}) {
		t.Fatalf("unused %v", u)
	}
	d := Docs(ports.Schema())
	if !strings.Contains(d, "when ports[] is a scalar") || !strings.Contains(d, "ports[].target") {
		t.Fatalf("docs:\n%s", d)
	}
}

func TestDictAndMapOf(t *testing.T) {
	env := MapOf("environment", Scalar(StringCodec))
	got, err := env.Build(mustJSON(t, `{"environment":{"A":"1","B":"2"}}`))
	if err != nil || !reflect.DeepEqual(got, map[string]string{"A": "1", "B": "2"}) {
		t.Fatalf("got %v err %v", got, err)
	}
	if u := env.Unused(mustJSON(t, `{"environment":{"anything":"x"}}`)); len(u) != 0 {
		t.Fatalf("dict must claim all keys, got %v", u)
	}
}

func TestRec(t *testing.T) {
	type Node struct {
		Name     string
		Children []Node
	}
	node := Rec("node", func(self Confy[Node]) Confy[Node] {
		return Struct[Node]().
			At(func(n *Node) *string { return &n.Name }, String("name")).
			At(func(n *Node) *[]Node { return &n.Children }, List("children", self))
	})
	got, err := node.Build(mustJSON(t, `{"name":"root","children":[{"name":"kid","children":[{"name":"leaf","children":[]}]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if got.Children[0].Children[0].Name != "leaf" {
		t.Fatalf("got %+v", got)
	}
	// Errors deep inside carry full paths.
	_, err = node.Build(mustJSON(t, `{"name":"root","children":[{"children":[]}]}`))
	if err == nil || !strings.Contains(err.Error(), "children[0].name: missing") {
		t.Fatalf("got %v", err)
	}
	s := node.Schema()
	if _, ok := s.Root.(Ref); !ok || s.Defs["node"] == nil {
		t.Fatalf("schema should be Ref + def: %+v", s)
	}
	if u := pathStrings(node.Unused(mustJSON(t, `{"name":"r","children":[{"name":"k","children":[],"zzz":1}]}`))); !reflect.DeepEqual(u, []string{"children[0].zzz"}) {
		t.Fatalf("unused %v", u)
	}
	js, _ := json.Marshal(JSONSchema(s))
	if !strings.Contains(string(js), `"$ref":"#/$defs/node"`) || !strings.Contains(string(js), `"$defs"`) {
		t.Fatalf("json schema: %s", js)
	}
	tpl := Template(s)
	if !strings.Contains(tpl, "recursive: node") {
		t.Fatalf("template:\n%s", tpl)
	}
	d := Docs(s)
	if !strings.Contains(d, "definitions:") || !strings.Contains(d, "node:children[]") {
		t.Fatalf("docs:\n%s", d)
	}
}

func TestSecretRedactsIssues(t *testing.T) {
	c := Int("token").Secret()
	_, err := c.Build(mustJSON(t, `{"token":"hunter2"}`))
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), "hunter2") {
		t.Fatalf("secret leaked: %v", err)
	}
	if !strings.Contains(err.Error(), "token: expected int: invalid value") {
		t.Fatalf("got %v", err)
	}
	// Non-secret includes the value for debuggability.
	_, err = Int("n").Build(mustJSON(t, `{"n":"abc"}`))
	if !strings.Contains(err.Error(), `"abc"`) {
		t.Fatalf("got %v", err)
	}
}

func TestMapErrAndCheckPaths(t *testing.T) {
	c := Under("db", String("url").MapErr(func(s string) (int, error) {
		if !strings.HasPrefix(s, "postgres://") {
			return 0, errors.New("must start with postgres://")
		}
		return len(s), nil
	}))
	_, err := c.Build(mustJSON(t, `{"db":{"url":"mysql://x"}}`))
	if err == nil || err.Error() != "db.url: must start with postgres://" {
		t.Fatalf("got %v", err)
	}
	// Check on a product reports at the product's cursor.
	pair := String("a").With(String("b"), func(a, b string) [2]string { return [2]string{a, b} }).
		Check(func(p [2]string) error {
			if p[0] == p[1] {
				return errors.New("a and b must differ")
			}
			return nil
		})
	_, err = Under("pair", pair).Build(mustJSON(t, `{"pair":{"a":"x","b":"x"}}`))
	if err == nil || err.Error() != "pair: a and b must differ" {
		t.Fatalf("got %v", err)
	}
}

func TestInlineUnionKubernetesStyle(t *testing.T) {
	// discriminator + optional members + cross-field check, no Switch needed.
	type U struct {
		Type   string
		FieldA *int
		FieldB *string
	}
	u := Struct[U]().
		At(func(u *U) *string { return &u.Type }, Field("type", Enum("A", "B"))).
		At(func(u *U) **int { return &u.FieldA }, Optional(Int("fieldA"))).
		At(func(u *U) **string { return &u.FieldB }, Optional(String("fieldB"))).
		Check(func(u U) error {
			switch {
			case u.Type == "A" && u.FieldA != nil && u.FieldB == nil, u.Type == "B" && u.FieldB != nil && u.FieldA == nil:
				return nil
			}
			return fmt.Errorf("type %s must set exactly its own member", u.Type)
		})
	if _, err := u.Build(mustJSON(t, `{"type":"A","fieldA":1}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := u.Build(mustJSON(t, `{"type":"A","fieldB":"x"}`)); err == nil {
		t.Fatal("expected check failure")
	}
	if _, err := u.Build(mustJSON(t, `{"type":"C"}`)); err == nil || !strings.Contains(err.Error(), "must be one of: A, B") {
		t.Fatalf("got %v", err)
	}
}

func TestPureUnusedIsEverything(t *testing.T) {
	u := pathStrings(Pure(1).Unused(mustJSON(t, `{"a":1,"b":2}`)))
	if !reflect.DeepEqual(u, []string{"a", "b"}) {
		t.Fatalf("got %v", u)
	}
}

func TestSwitchDuplicateArmPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic")
		}
	}()
	Switch("k", Arm("a", Pure(1)), Arm("a", Pure(2)))
}

func TestBuildDoesNotReportUnusedUnderRejectedSubtree(t *testing.T) {
	// A mapping where a scalar is expected: one error, not one per key.
	_, err := Int("n").Build(mustJSON(t, `{"n": {"a": 1, "b": 2}}`))
	if err == nil || err.Error() != "n: expected int, got mapping" {
		t.Fatalf("got %v", err)
	}
	// Same for a sequence expected and a Shape with no arm for the kind.
	_, err = List("xs", Int("v")).Build(mustJSON(t, `{"xs": {"a": 1}}`))
	if err == nil || err.Error() != "xs: expected sequence, got mapping" {
		t.Fatalf("got %v", err)
	}
	// A validation failure is not a rejection: unused keys beneath it still show.
	pair := Under("pair", String("a").With(String("b"), func(a, b string) [2]string { return [2]string{a, b} }).
		Check(func(p [2]string) error { return errors.New("bad pair") }))
	_, err = pair.Build(mustJSON(t, `{"pair": {"a": "x", "b": "y", "c": "z"}}`))
	if err == nil || err.Error() != "pair: bad pair\npair.c: unused key" {
		t.Fatalf("got %v", err)
	}
}

func TestAllowUnusedMatching(t *testing.T) {
	ext := AllowUnusedMatching(func(p Path) bool { return strings.HasPrefix(p[len(p)-1].Key, "x-") })
	v := mustJSON(t, `{"host": "h", "x-team": "core", "typo": 1, "tls": {"mode": "off", "x-note": "n"}, "upstreams": []}`)
	_, err := serverConfy().Build(v, ext)
	if err == nil || err.Error() != "typo: unused key" {
		t.Fatalf("got %v", err)
	}
	if _, err := serverConfy().Build(v, AllowUnused()); err != nil {
		t.Fatalf("AllowUnused should accept everything, got %v", err)
	}
}

package example_test

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/yiblet/confy"
)

// Caddy-style routes: a list of handlers discriminated by a "handler" tag,
// one of which ("subroute") contains routes again. Recursion through a
// tagged union, kept finite in the schema by Rec.

type Handler interface{ isHandler() }

type StaticResponse struct {
	Body   string
	Status int
}
type ReverseProxy struct{ Upstreams []string }
type Subroute struct{ Routes []Route }

func (StaticResponse) isHandler() {}
func (ReverseProxy) isHandler()   {}
func (Subroute) isHandler()       {}

type Route struct {
	Match  []string // host matchers
	Handle []Handler
}

func routesConfy() confy.Confy[[]Route] {
	return confy.Rec("routes", func(self confy.Confy[[]Route]) confy.Confy[[]Route] {
		handler := confy.Switch("handler",
			confy.Case("static_response",
				confy.Struct[StaticResponse]().
					At(func(s *StaticResponse) *string { return &s.Body }, confy.String("body").Default("")).
					At(func(s *StaticResponse) *int { return &s.Status }, confy.Int("status_code").Default(200)),
				func(s StaticResponse) Handler { return s }),
			confy.Case("reverse_proxy",
				confy.Struct[ReverseProxy]().
					At(func(r *ReverseProxy) *[]string { return &r.Upstreams }, confy.List("upstreams", confy.String("dial"))),
				func(r ReverseProxy) Handler { return r }),
			confy.Case("subroute",
				confy.Struct[Subroute]().
					At(func(s *Subroute) *[]Route { return &s.Routes }, self),
				func(s Subroute) Handler { return s }),
		)
		route := confy.Struct[Route]().
			At(func(r *Route) *[]string { return &r.Match },
				confy.Optional(confy.List("match", confy.String("host"))).Map(func(m *[]string) []string {
					if m == nil {
						return nil
					}
					return *m
				})).
			At(func(r *Route) *[]Handler { return &r.Handle }, confy.List("handle", handler))
		return confy.List("routes", route)
	})
}

func describe(rs []Route, depth int) {
	pad := strings.Repeat("  ", depth)
	for _, r := range rs {
		fmt.Printf("%sroute match=%v\n", pad, r.Match)
		for _, h := range r.Handle {
			switch h := h.(type) {
			case StaticResponse:
				fmt.Printf("%s  static %d %q\n", pad, h.Status, h.Body)
			case ReverseProxy:
				fmt.Printf("%s  proxy -> %v\n", pad, h.Upstreams)
			case Subroute:
				fmt.Printf("%s  subroute:\n", pad)
				describe(h.Routes, depth+2)
			}
		}
	}
}

func Example_caddyRecursiveRoutes() {
	rs, err := routesConfy().Build(parse(`{"routes": [
	  {"match": [{"host": "example.com"}], "handle": [
	    {"handler": "subroute", "routes": [
	      {"match": [{"host": "api.example.com"}], "handle": [
	        {"handler": "reverse_proxy", "upstreams": [{"dial": "10.0.0.1:8080"}, {"dial": "10.0.0.2:8080"}]}
	      ]},
	      {"handle": [{"handler": "static_response", "body": "hello", "status_code": 200}]}
	    ]}
	  ]},
	  {"handle": [{"handler": "static_response", "status_code": 404}]}
	]}`))
	fmt.Println("err:", err)
	describe(rs, 0)

	// Output:
	// err: <nil>
	// route match=[example.com]
	//   subroute:
	//     route match=[api.example.com]
	//       proxy -> [10.0.0.1:8080 10.0.0.2:8080]
	//     route match=[]
	//       static 200 "hello"
	// route match=[]
	//   static 404 ""
}

func Example_caddyDeepErrorsAndUnusedKeys() {
	v := parse(`{"routes": [
	  {"handle": [
	    {"handler": "subroute", "routes": [
	      {"handle": [{"handler": "reverse_proxy", "upstreams": [{"dial": "a"}, {"dail": "b"}]}]},
	      {"handle": [{"handler": "file_server"}]}
	    ]}
	  ]}
	]}`)
	_, err := routesConfy().Build(v)
	fmt.Println(err)

	// Output:
	// routes[0].handle[0].routes[0].handle[0].upstreams[1].dial: missing
	// routes[0].handle[0].routes[1].handle[0].handler: unknown tag "file_server" (one of: static_response, reverse_proxy, subroute)
	// routes[0].handle[0].routes[0].handle[0].upstreams[1].dail: unused key
}

func Example_caddySchemaStaysFinite() {
	s := routesConfy().Schema()
	fmt.Print(confy.Docs(s))
	fmt.Println("---")
	js, _ := json.Marshal(confy.JSONSchema(s))
	// Just the shape of the recursion: a $ref back into $defs.
	fmt.Println(strings.Contains(string(js), `"$ref":"#/$defs/routes"`), strings.Count(string(js), `"const":`))

	// Output:
	// (value)  → routes
	//
	// definitions:
	// routes:routes[].match[].host               string                                            required  if routes:routes[].match is set
	// routes:routes[].handle[].handler           one of: static_response, reverse_proxy, subroute  required
	// routes:routes[].handle[].body              string                                            default      when routes:routes[].handle[].handler=static_response
	// routes:routes[].handle[].status_code       int                                               default 200  when routes:routes[].handle[].handler=static_response
	// routes:routes[].handle[].upstreams[].dial  string                                            required     when routes:routes[].handle[].handler=reverse_proxy
	// routes:routes[].handle[]                   → routes                                                       when routes:routes[].handle[].handler=subroute
	// ---
	// true 3
}

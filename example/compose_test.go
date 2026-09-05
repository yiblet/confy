package example_test

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/yiblet/confy"
)

// Compose-style services: the same field accepts a short scalar form or a
// long mapping form, discriminated by the kind of the node, never by a tag.

type Port struct {
	Target    int
	Published int
	Protocol  string
}

type Service struct {
	Image       string
	Command     []string
	Ports       []Port
	Environment map[string]string
	DependsOn   []string
}

// portConfy reads "8080:80" or {target: 80, published: 8080, protocol: tcp}.
func portConfy() confy.Confy[Port] {
	short := confy.Scalar(confy.StringCodec).MapErr(func(s string) (Port, error) {
		pub, tgt, ok := strings.Cut(s, ":")
		if !ok {
			tgt, pub = pub, tgt
		}
		var p Port
		var err error
		if p.Target, err = strconv.Atoi(tgt); err != nil {
			return p, fmt.Errorf("bad target port %q", tgt)
		}
		if pub != "" {
			if p.Published, err = strconv.Atoi(pub); err != nil {
				return p, fmt.Errorf("bad published port %q", pub)
			}
		}
		p.Protocol = "tcp"
		return p, nil
	})
	long := confy.Struct[Port]().
		At(func(p *Port) *int { return &p.Target }, confy.Int("target")).
		At(func(p *Port) *int { return &p.Published }, confy.Int("published").Default(0)).
		At(func(p *Port) *string { return &p.Protocol }, confy.Field("protocol", confy.Enum("tcp", "udp")).Default("tcp"))
	return confy.Shape(
		confy.KindArm(confy.KindScalar, short),
		confy.KindArm(confy.KindMapping, long),
	)
}

// stringOrList reads "a b c" or ["a", "b", "c"].
func stringOrList() confy.Confy[[]string] {
	return confy.Shape(
		confy.KindCase(confy.KindScalar, confy.Scalar(confy.StringCodec), strings.Fields),
		confy.KindArm(confy.KindSequence, confy.Seq(confy.Scalar(confy.StringCodec))),
	)
}

// envConfy reads {A: "1"} or ["A=1", "B=2"].
func envConfy() confy.Confy[map[string]string] {
	return confy.Shape(
		confy.KindArm(confy.KindMapping, confy.Dict(confy.Scalar(confy.StringCodec))),
		confy.KindCase(confy.KindSequence, confy.Seq(confy.Scalar(confy.StringCodec)), func(kvs []string) map[string]string {
			m := map[string]string{}
			for _, kv := range kvs {
				k, v, _ := strings.Cut(kv, "=")
				m[k] = v
			}
			return m
		}),
	)
}

func serviceConfy() confy.Confy[Service] {
	return confy.Struct[Service]().
		At(func(s *Service) *string { return &s.Image }, confy.String("image")).
		At(func(s *Service) *[]string { return &s.Command }, confy.Under("command", stringOrList()).Doc("Overrides the image CMD")).
		At(func(s *Service) *[]Port { return &s.Ports }, confy.List("ports", portConfy())).
		At(func(s *Service) *map[string]string { return &s.Environment }, confy.Under("environment", envConfy())).
		At(func(s *Service) *[]string { return &s.DependsOn }, confy.List("depends_on", confy.Scalar(confy.StringCodec)))
}

func parse(js string) confy.Value {
	var v any
	if err := json.Unmarshal([]byte(js), &v); err != nil {
		panic(err)
	}
	return confy.FromAny(v)
}

func Example_composeShortAndLongSyntax() {
	web := parse(`{
	  "image": "nginx",
	  "command": "nginx -g 'daemon off;'",
	  "ports": ["8080:80", {"target": 443, "published": 8443, "protocol": "tcp"}, "9000"],
	  "environment": ["RACK_ENV=development", "DEBUG=1"],
	  "depends_on": ["db", "redis"]
	}`)
	svc, err := serviceConfy().Build(web)
	fmt.Println("err:", err)
	fmt.Printf("image=%s command=%q\n", svc.Image, svc.Command)
	for _, p := range svc.Ports {
		fmt.Printf("port %+v\n", p)
	}
	fmt.Println("env:", svc.Environment, "depends_on:", svc.DependsOn)

	// The long form of the same service reads identically.
	long := parse(`{
	  "image": "nginx",
	  "command": ["nginx", "-g", "daemon off;"],
	  "ports": [{"target": 80, "published": 8080}],
	  "environment": {"RACK_ENV": "development", "DEBUG": "1"},
	  "depends_on": ["db"]
	}`)
	svc, err = serviceConfy().Build(long)
	fmt.Println("long form err:", err)
	fmt.Println("long form env:", svc.Environment, "ports:", svc.Ports)

	// Output:
	// err: <nil>
	// image=nginx command=["nginx" "-g" "'daemon" "off;'"]
	// port {Target:80 Published:8080 Protocol:tcp}
	// port {Target:443 Published:8443 Protocol:tcp}
	// port {Target:9000 Published:0 Protocol:tcp}
	// env: map[DEBUG:1 RACK_ENV:development] depends_on: [db redis]
	// long form err: <nil>
	// long form env: map[DEBUG:1 RACK_ENV:development] ports: [{80 8080 tcp}]
}

func Example_composeShapeErrors() {
	// A kind no arm accepts, a bad short port, and an unknown protocol: all
	// reported in one pass, each at its own path.
	_, err := serviceConfy().Build(parse(`{
	  "image": "nginx",
	  "command": {"not": "allowed"},
	  "ports": ["eighty:80", {"target": 80, "protocol": "sctp"}],
	  "environment": {},
	  "depends_on": []
	}`))
	fmt.Println(err)

	// Output:
	// command: expected scalar or sequence, got mapping
	// ports[0]: bad published port "eighty"
	// ports[1].protocol: expected one of: tcp, udp, got "sctp": must be one of: tcp, udp
}

func Example_composeDocsShowBothForms() {
	fmt.Print(confy.Docs(serviceConfy().Schema()))

	// Output:
	// image              string            required
	// command            string            required     when command is a scalar
	//     Overrides the image CMD
	// command[]          string            required     when command is a sequence
	// ports[]            string            required     when ports[] is a scalar
	// ports[].target     int               required     when ports[] is a mapping
	// ports[].published  int               default 0    when ports[] is a mapping
	// ports[].protocol   one of: tcp, udp  default tcp  when ports[] is a mapping
	// environment.<key>  string            required     when environment is a mapping
	// environment[]      string            required     when environment is a sequence
	// depends_on[]       string            required
}

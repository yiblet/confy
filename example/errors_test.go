package example_test

import (
	"errors"
	"fmt"

	"github.com/yiblet/confy"
)

// Error reporting: every issue in one pass, addressable individually.

func Example_errorsAllAtOnce() {
	// Reuse the compose service reader with everything wrong at once.
	_, err := serviceConfy().Build(parse(`{
	  "image": 42,
	  "command": ["ok"],
	  "ports": [{"published": "x"}],
	  "environment": "not-a-map",
	  "extra": true
	}`))

	var issues confy.Issues
	if errors.As(err, &issues) {
		fmt.Println(len(issues), "issues")
	}
	for _, iss := range issues {
		fmt.Printf("  %-22s %s\n", iss.Path, iss.Msg)
	}

	// Output:
	// 5 issues
	//   ports[0].target        missing
	//   ports[0].published     expected int, got "x": strconv.ParseInt: parsing "x": invalid syntax
	//   environment            expected mapping or sequence, got scalar
	//   depends_on             missing
	//   extra                  unused key
}

func Example_errorsStrictMode() {
	// Build adds unused keys; a typo'd key shows up next to what is
	// missing, which is usually the same mistake seen twice.
	_, err := serviceConfy().Build(parse(`{
	  "image": "nginx",
	  "comand": "true",
	  "ports": [],
	  "environment": {},
	  "depends_on": []
	}`))
	fmt.Println(err)

	// Output:
	// command: missing
	// comand: unused key
}

func Example_errorsPathsAreStructured() {
	// Paths are data, not strings: filter or group them.
	_, err := routesConfy().Build(parse(`{"routes": [
	  {"handle": [{"handler": "reverse_proxy", "upstreams": [{}, {}]}]},
	  {"handle": [{"handler": "nope"}]}
	]}`))
	var issues confy.Issues
	errors.As(err, &issues)
	byRoute := map[int]int{}
	for _, iss := range issues {
		byRoute[iss.Path[1].Index]++ // routes[N]
	}
	fmt.Println("issues per route:", byRoute)
	fmt.Println("first step:", issues[0].Path[0].Key, "depth:", len(issues[0].Path))

	// Output:
	// issues per route: map[0:2 1:1]
	// first step: routes depth: 7
}

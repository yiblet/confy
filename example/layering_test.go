package example_test

import (
	"flag"
	"fmt"
	"strings"

	"github.com/yiblet/confy"
)

// Source precedence (defaults < base file < override file < flags) is a
// property of the Value, not of the reader. Layer merges Values; the reader
// never learns where a value came from.

func Example_layeringFilesAndFlags() {
	cfg := appConfy()

	file := parse(`{
	  "env": "staging",
	  "listen": ":9090",
	  "db": {"host": "db.file", "user": "file-user", "password": "pw"},
	  "features": ["a"],
	  "labels": {"owner": "file"}
	}`)

	override := parse(`{"db": {"host": "db.override"}, "labels": {"region": "eu"}}`)

	// Flags: a FlagSet is itself a schema-as-value; walk it into a nested
	// map with dotted names so it becomes one more Value.
	fs := flag.NewFlagSet("app", flag.ContinueOnError)
	fs.String("db.host", "", "database host")
	fs.Int("db.port", 0, "database port")
	fs.String("listen", "", "bind address")
	_ = fs.Parse([]string{"-db.port=6543", "-listen=:80"})
	flags := map[string]any{}
	fs.Visit(func(f *flag.Flag) { // only flags actually set
		segs := strings.Split(f.Name, ".")
		m := flags
		for _, s := range segs[:len(segs)-1] {
			next, ok := m[s].(map[string]any)
			if !ok {
				next = map[string]any{}
				m[s] = next
			}
			m = next
		}
		m[segs[len(segs)-1]] = f.Value.String()
	})

	merged := confy.Layer(confy.Layer(file, override), confy.FromAny(flags))
	app, err := cfg.Build(merged)
	fmt.Println("err:", err)
	fmt.Printf("env=%s (file)\n", app.Env)
	fmt.Printf("listen=%s (flag beats file)\n", app.Listen)
	fmt.Printf("db.host=%s (override beats base)\n", app.DB.Host)
	fmt.Printf("db.port=%d (flag; codec parsed the string)\n", app.DB.Port)
	fmt.Printf("labels=%v (mappings merge key-wise)\n", app.Labels)

	// Output:
	// err: <nil>
	// env=staging (file)
	// listen=:80 (flag beats file)
	// db.host=db.override (override beats base)
	// db.port=6543 (flag; codec parsed the string)
	// labels=map[owner:file region:eu] (mappings merge key-wise)
}

func Example_layeringUnusedKeysAcrossSources() {
	// Unused-key detection sees the merged Value, so a typo in any source
	// surfaces, with the full path.
	cfg := appConfy()
	file := parse(`{"env": "dev", "db": {"host": "h", "user": "u", "password": "p", "poolsize": 5}, "features": [], "labels": {}}`)
	over := parse(`{"listn": ":1"}`)
	_, err := cfg.Build(confy.Layer(file, over))
	fmt.Println(err)

	// Output:
	// db.poolsize: unused key
	// listn: unused key
}

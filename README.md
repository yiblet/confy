# Confy

Confy is a Go library for building configuration.

* **Low dependencies:** Confy depends only on the standard library. JSON works out of the box. TOML, YAML, or anything else is the parser you already use, passed as a function, so Confy adds nothing to your build.
* **Modular:** Each package declares the configuration it needs and how that becomes a working component. You compose them to make the whole binary, and `Build` hands back wired components, not a bag of settings. Configuration scales with your code instead of piling up in one central struct.
* **Easy to use:** From one configuration definition, Confy can generate docs, JSON schema, and easy to read error messages.

Confy requires Go 1.27 or later. Install it with `go get github.com/yiblet/confy`.

## Declare configuration where it belongs

A package that needs configuration exports a `Confy` for the component it provides. It reads its settings into a private struct, then `MapErr` turns them into the real thing. The result is a `Confy[*Cache]`, not a `Confy[CacheConfig]`. The reader is an ordinary value. It doesn't know where it will end up in the final config, or which other packages exist.

```go
package cache

type settings struct {
	addr string
	ttl  time.Duration
	size int
}

func Confy() confy.Confy[*Cache] {
	return confy.Struct[settings]().
		At(func(s *settings) *string { return &s.addr }, confy.String("addr").Doc("Redis address")).
		At(func(s *settings) *time.Duration { return &s.ttl }, confy.Duration("ttl").Default(5*time.Minute)).
		At(func(s *settings) *int { return &s.size }, confy.Int("size").Default(1000).Check(positive)).
		MapErr(func(s settings) (*Cache, error) { return New(s.addr, s.ttl, s.size) })
}
```

Each `At` fills one field. The selector returns a pointer to the field, so the compiler checks that the reader's type matches. There is no reflection and there are no struct tags. The constructor only runs when every field is valid, and an error from it is reported at the module's path.

## Compose packages into one binary

Your `main` package puts the modules together. Adding one is a single line. If two modules claim the same key, Confy panics at startup instead of letting one silently win.

```go
type App struct {
	Listen string
	Cache  *cache.Cache
	DB     *db.DB
}

app := confy.Struct[App]().
	At(func(a *App) *string { return &a.Listen }, confy.String("listen").Default(":8080")).
	At(func(a *App) **cache.Cache { return &a.Cache }, confy.Under("cache", cache.Confy())).
	At(func(a *App) **db.DB { return &a.DB }, confy.Under("db", db.Confy()))
```

`Build` returns an `App` whose cache and database are already constructed. Your `main` reads a file and starts serving.

## Let the binary explain itself

Because the reader is a value, Confy can read its description without any config file in hand, and without running a single constructor. `confy.Docs` lists every key the assembled binary reads, with its type, default, and documentation. Wire it to a `--config-docs` flag and operators never need the source.

```
listen       string    default :8080
cache.addr   string    required
    Redis address
cache.ttl    duration  default 5m0s
cache.size   int       default 1000
db.url       string    required
    postgres:// URL
db.password  string    required  [secret]
db.pool      int       default 10
```

`confy.Template` writes a config file with every key in place, ready to fill in. `confy.JSONSchema` returns a draft 2020-12 schema, so editors can complete keys and CI can validate files before a deploy.

```yaml
listen: :8080
cache:
  # Redis address
  addr: <string>
  ttl: 5m0s
  size: 1000
db:
  # postgres:// URL
  url: <string>
  password: <secret>
  pool: 10
```

## Catch every mistake at once

`Build` reads a config and reports everything wrong with it in one pass. A key that no module reads is an error too, so a typo fails the build instead of quietly falling back to a default. When every field is valid and a constructor still refuses, that lands at the module's path: `db: url scheme must be postgres, got "mysql"`.

```go
cfg, err := app.LoadJSONFile("app.json")
```

```
cache.addr: missing
cache.size: size must be positive
db.password: missing
db.pasword: unused key
```

You decide how strict to be. Pass `confy.AllowUnused()` when a reader deliberately covers part of a file, or `confy.AllowUnusedMatching(f)` to permit only the keys your format reserves for extensions, such as compose's `x-` prefix.

## Bring your own format

Confy reads the tree your parser produces, but it doesn't prescribe which parser. JSON is built in because the standard library has it. For everything else, pass the `Unmarshal` function or the `NewDecoder` constructor from the library you already depend on.

```go
cfg, err := app.LoadJSONFile("app.json")
cfg, err := app.Load(data, toml.Unmarshal)
cfg, err := app.Load(data, yaml.Unmarshal)
cfg, err := app.Decode(r, toml.NewDecoder)
```

Any parser that produces `map[string]any`, `[]any`, and scalars works. If you already have a decoded tree, or you want to layer several sources before building, wrap it with `confy.FromAny` and call `Build`.

```go
v := confy.Layer(confy.Layer(fileValue, overrideValue), flagValue)
cfg, err := app.Build(v)
```

## Model the shapes real configs have

Real configuration has alternatives, short forms, and recursion. Confy gives each one a combinator, and every combinator shows up in the docs, the template, and the schema like any other. `Switch` chooses an arm by a tag key, and each `Case` says how its variant widens to your interface, so the compiler checks membership.

```go
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
```

```
tls.mode         one of: off, acme, manual  required
tls.email        string                     required  when tls.mode=acme
tls.cert         string                     required  when tls.mode=manual
tls.key          string                     required  when tls.mode=manual  [secret]
```

`Shape` accepts a short form and a long form by dispatching on whether the node is a scalar, a mapping, or a sequence, the way compose lets `ports` be `"8080:80"` or `{target: 80}`. `Rec` ties the knot for configs that contain themselves, like Caddy routes, and keeps the schema finite. `Under` nests, `List` and `MapOf` collect, `Optional` tolerates absence, and `Check` validates a field or a whole struct. Defaults live in the schema, so the docs show them, and `Secret` keeps a value out of every error message and template.

```go
confy.Int("port").Default(8080).Check(inRange)
confy.String("url").MapErr(url.Parse)
confy.String("password").Secret()
confy.Field("env", confy.Enum("dev", "staging", "prod"))
confy.Optional(confy.List("upstreams", confy.String("url")))
```

The `example` directory has runnable examples with pinned output for compose short and long syntax, Kubernetes inline unions, Caddy recursive routes, source layering, secrets, custom codecs, and error reporting. The API reference is on [pkg.go.dev](https://pkg.go.dev/github.com/yiblet/confy). Confy is [MIT licensed](./LICENSE).

package modular_test

import (
	"fmt"

	"github.com/yiblet/confy"
	"github.com/yiblet/confy/example/modular/cache"
	"github.com/yiblet/confy/example/modular/db"
)

// The binary composes the modules' Confys. Each yields a built component, so
// Build returns a wired App. Adding a module is one At line; the docs,
// template, schema and unused-key detection all follow.

type App struct {
	Listen string
	Cache  *cache.Cache
	DB     *db.DB
}

func appConfy() confy.Confy[App] {
	return confy.Struct[App]().
		At(func(a *App) *string { return &a.Listen }, confy.String("listen").Default(":8080")).
		At(func(a *App) **cache.Cache { return &a.Cache }, confy.Under("cache", cache.Confy())).
		At(func(a *App) **db.DB { return &a.DB }, confy.Under("db", db.Confy()))
}

func Example_buildReturnsWiredComponents() {
	app, err := appConfy().LoadJSON([]byte(`{
	  "cache": {"addr": "redis:6379"},
	  "db": {"url": "postgres://db.internal/app", "password": "s3cret"}
	}`))
	fmt.Println("err:", err)
	fmt.Println(app.Listen, app.Cache.Addr(), app.DB.Host())

	// Output:
	// err: <nil>
	// :8080 redis:6379 db.internal
}

// Example_binaryDescribesItself is what `./svc --config-docs` would print:
// the complete configuration of the assembled binary, from the modules'
// own declarations. MapErr never runs here; the schema is static.
func Example_binaryDescribesItself() {
	fmt.Print(confy.Docs(appConfy().Schema()))

	// Output:
	// listen       string    default :8080
	// cache.addr   string    required
	//     Redis address
	// cache.ttl    duration  default 5m0s
	// cache.size   int       default 1000
	// db.url       string    required
	//     postgres:// URL
	// db.password  string    required  [secret]
	// db.pool      int       default 10
}

func Example_binaryTemplate() {
	fmt.Print(confy.Template(appConfy().Schema()))

	// Output:
	// listen: :8080
	// cache:
	//   # Redis address
	//   addr: <string>
	//   ttl: 5m0s
	//   size: 1000
	// db:
	//   # postgres:// URL
	//   url: <string>
	//   password: <secret>
	//   pool: 10
}

func Example_modulesFailTogether() {
	// Every module's problems surface in one run, each under its own prefix.
	_, err := appConfy().LoadJSON([]byte(`{
	  "cache": {"size": 0},
	  "db": {"url": "postgres://x", "pasword": "p"}
	}`))
	fmt.Println(err)

	// Output:
	// cache.addr: missing
	// cache.size: size must be positive
	// db.password: missing
	// db.pasword: unused key
}

func Example_constructorErrorsLandOnTheModule() {
	// The fields are all valid, so each module's constructor runs. A failure
	// there is reported at the module's own path.
	_, err := appConfy().LoadJSON([]byte(`{
	  "cache": {"addr": "redis:6379", "ttl": "100ms"},
	  "db": {"url": "mysql://x", "password": "p"}
	}`))
	fmt.Println(err)

	// Output:
	// cache: ttl 100ms is below the 1s minimum
	// db: url scheme must be postgres, got "mysql"
}

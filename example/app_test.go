package example_test

import (
	"fmt"
	"time"

	"github.com/yiblet/confy"
)

// A small application config shared by the layering and secrets examples.

type Database struct {
	Host     string
	Port     int
	User     string
	Password string
	PoolSize int
}

type App struct {
	Env      string
	Listen   string
	Timeout  time.Duration
	DB       Database
	Features []string
	Labels   map[string]string
}

func appConfy() confy.Confy[App] {
	db := confy.Struct[Database]().
		At(func(d *Database) *string { return &d.Host }, confy.String("host")).
		At(func(d *Database) *int { return &d.Port }, confy.Int("port").Default(5432)).
		At(func(d *Database) *string { return &d.User }, confy.String("user")).
		At(func(d *Database) *string { return &d.Password }, confy.String("password").Secret()).
		At(func(d *Database) *int { return &d.PoolSize }, confy.Int("pool_size").Default(10).Check(func(n int) error {
			if n < 1 || n > 100 {
				return fmt.Errorf("pool_size %d not in 1..100", n)
			}
			return nil
		}))
	return confy.Struct[App]().
		At(func(a *App) *string { return &a.Env }, confy.Field("env", confy.Enum("dev", "staging", "prod")).Default("dev")).
		At(func(a *App) *string { return &a.Listen }, confy.String("listen").Default(":8080").Doc("host:port to bind")).
		At(func(a *App) *time.Duration { return &a.Timeout }, confy.Duration("timeout").Default(30*time.Second)).
		At(func(a *App) *Database { return &a.DB }, confy.Under("db", db)).
		At(func(a *App) *[]string { return &a.Features }, confy.List("features", confy.Scalar(confy.StringCodec))).
		At(func(a *App) *map[string]string { return &a.Labels }, confy.MapOf("labels", confy.Scalar(confy.StringCodec)))
}

func Example_appDefaultsAndDocs() {
	app, err := appConfy().Build(parse(`{"db": {"host": "h", "user": "u", "password": "p"}, "features": [], "labels": {}}`))
	fmt.Println("err:", err)
	fmt.Printf("env=%s listen=%s timeout=%s db.port=%d pool=%d\n", app.Env, app.Listen, app.Timeout, app.DB.Port, app.DB.PoolSize)
	fmt.Print(confy.Docs(appConfy().Schema()))

	// Output:
	// err: <nil>
	// env=dev listen=:8080 timeout=30s db.port=5432 pool=10
	// env           one of: dev, staging, prod  default dev
	// listen        string                      default :8080
	//     host:port to bind
	// timeout       duration                    default 30s
	// db.host       string                      required
	// db.port       int                         default 5432
	// db.user       string                      required
	// db.password   string                      required  [secret]
	// db.pool_size  int                         default 10
	// features[]    string                      required
	// labels.<key>  string                      required
}

func Example_secretsNeverLeakIntoErrors() {
	v := parse(`{
	  "env": "production",
	  "db": {"host": "h", "user": "u", "password": 12345, "pool_size": "nine-hundred", "port": "secret-port"},
	  "features": [], "labels": {}
	}`)
	_, err := appConfy().Build(v)
	fmt.Println(err)

	// Mark a whole block secret and every value inside disappears from the
	// messages too.
	secretDB := confy.Struct[App]().
		At(func(a *App) *Database { return &a.DB }, confy.Under("db",
			confy.Struct[Database]().At(func(d *Database) *int { return &d.Port }, confy.Int("port"))).Secret())
	_, err = secretDB.Build(v, confy.AllowUnused()) // partial reader: the rest of the file is expected
	fmt.Println(err)

	// Output:
	// env: expected one of: dev, staging, prod, got "production": must be one of: dev, staging, prod
	// db.port: expected int, got "secret-port": strconv.ParseInt: parsing "secret-port": invalid syntax
	// db.pool_size: expected int, got "nine-hundred": strconv.ParseInt: parsing "nine-hundred": invalid syntax
	// db.port: expected int: invalid value
}

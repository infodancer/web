module github.com/infodancer/web/antibot

go 1.26.10

replace github.com/infodancer/web/pgtest => ../pgtest

require (
	github.com/infodancer/logging/httplog v0.1.2
	github.com/infodancer/web/pgtest v0.1.0
	github.com/jackc/pgx/v5 v5.10.0
	github.com/pressly/goose/v3 v3.28.0
)

require (
	github.com/felixge/httpsnoop v1.1.0 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	github.com/mfridman/interpolate v0.0.2 // indirect
	github.com/sethvargo/go-retry v0.4.0 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	golang.org/x/sync v0.22.0 // indirect
	golang.org/x/text v0.41.0 // indirect
)

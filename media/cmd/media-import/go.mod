module github.com/infodancer/web/media/cmd/media-import

go 1.26.10

// media is unpublished at the version carrying Ingest; resolve it from the
// parent tree (the go.work workspace also wires this for local dev).
replace github.com/infodancer/web/media => ../..

require (
	github.com/infodancer/web/media v0.0.0
	github.com/jackc/pgx/v5 v5.9.2
)

require (
	github.com/HugoSmits86/nativewebp v1.3.0 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	github.com/mfridman/interpolate v0.0.2 // indirect
	github.com/pressly/goose/v3 v3.27.1 // indirect
	github.com/sethvargo/go-retry v0.3.0 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	golang.org/x/image v0.45.0 // indirect
	golang.org/x/sync v0.24.0 // indirect
	golang.org/x/text v0.43.0 // indirect
	modernc.org/libc v1.72.3 // indirect
)

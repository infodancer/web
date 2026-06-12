module github.com/infodancer/web/blog

go 1.26.4

require (
	github.com/infodancer/authz v0.1.0
	github.com/infodancer/ui v0.6.0
	github.com/infodancer/ui/markdown v0.2.1
	github.com/infodancer/ui/mdedit v0.2.4
	github.com/infodancer/web/media v0.1.3
	github.com/jackc/pgx/v5 v5.9.2
	github.com/pressly/goose/v3 v3.27.1
	golang.org/x/text v0.37.0
)

require (
	github.com/HugoSmits86/nativewebp v1.3.0 // indirect
	github.com/aymerick/douceur v0.2.0 // indirect
	github.com/gorilla/css v1.0.1 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	github.com/mfridman/interpolate v0.0.2 // indirect
	github.com/microcosm-cc/bluemonday v1.0.27 // indirect
	github.com/sethvargo/go-retry v0.3.0 // indirect
	github.com/yuin/goldmark v1.8.2 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	golang.org/x/image v0.39.0 // indirect
	golang.org/x/net v0.55.0 // indirect
	golang.org/x/sync v0.20.0 // indirect
)

replace github.com/infodancer/web/media => ../media

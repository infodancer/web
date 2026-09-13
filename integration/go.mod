module github.com/infodancer/web/integration

go 1.26.6

replace github.com/infodancer/web/contact => ../contact

replace github.com/infodancer/web/newsletter => ../newsletter

replace github.com/infodancer/web/faq => ../faq

replace github.com/infodancer/web/search => ../search

replace github.com/infodancer/web/notify => ../notify

require (
	github.com/infodancer/smoke v0.1.0
	github.com/infodancer/web/contact v0.0.0-00010101000000-000000000000
	github.com/infodancer/web/faq v0.0.0-00010101000000-000000000000
	github.com/infodancer/web/media v0.1.3
	github.com/infodancer/web/newsletter v0.0.0-00010101000000-000000000000
	github.com/infodancer/web/notify v0.0.0-00010101000000-000000000000
	github.com/infodancer/web/search v0.0.0-00010101000000-000000000000
	github.com/jackc/pgx/v5 v5.10.0
)

require (
	cuelang.org/go v0.16.0 // indirect
	github.com/HugoSmits86/nativewebp v1.3.0 // indirect
	github.com/PuerkitoBio/goquery v1.12.0 // indirect
	github.com/andybalholm/cascadia v1.3.3 // indirect
	github.com/aymerick/douceur v0.2.0 // indirect
	github.com/cockroachdb/apd/v3 v3.2.1 // indirect
	github.com/emicklei/proto v1.14.3 // indirect
	github.com/felixge/httpsnoop v1.1.0 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/gorilla/css v1.0.1 // indirect
	github.com/hashicorp/golang-lru/v2 v2.0.7 // indirect
	github.com/infodancer/logging/httplog v0.1.2 // indirect
	github.com/infodancer/ui v0.6.1 // indirect
	github.com/infodancer/ui/markdown v0.2.1 // indirect
	github.com/infodancer/ui/mdedit v0.2.4 // indirect
	github.com/infodancer/web/antibot v0.1.1 // indirect
	github.com/matthewjhunter/go-embedding v0.6.1 // indirect
	github.com/microcosm-cc/bluemonday v1.0.27 // indirect
	github.com/mitchellh/go-wordwrap v1.0.1 // indirect
	github.com/pelletier/go-toml/v2 v2.3.1 // indirect
	github.com/protocolbuffers/txtpbfmt v0.0.0-20260217160748-a481f6a22f94 // indirect
	github.com/yuin/goldmark v1.8.2 // indirect
	go.yaml.in/yaml/v3 v3.0.5 // indirect
	golang.org/x/image v0.45.0 // indirect
	golang.org/x/net v0.58.0 // indirect
	google.golang.org/protobuf v1.36.12 // indirect
)

require (
	github.com/infodancer/web/blog v0.0.0
	github.com/infodancer/web/pgtest v0.1.0
	github.com/infodancer/web/profile v0.0.0-00010101000000-000000000000
	github.com/infodancer/web/timeline v0.0.0-00010101000000-000000000000
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	github.com/mfridman/interpolate v0.0.2 // indirect
	github.com/pressly/goose/v3 v3.28.0 // indirect
	github.com/sethvargo/go-retry v0.4.0 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	golang.org/x/sync v0.22.0 // indirect
	golang.org/x/text v0.41.0 // indirect
)

replace github.com/infodancer/web/timeline => ../timeline

replace github.com/infodancer/web/media => ../media

replace github.com/infodancer/web/profile => ../profile

replace github.com/infodancer/web/blog => ../blog

replace github.com/infodancer/web/pgtest => ../pgtest

replace github.com/infodancer/web/antibot => ../antibot

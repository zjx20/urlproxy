module github.com/zjx20/urlproxy

go 1.19

require golang.org/x/net v0.5.0

require (
	github.com/etherlabsio/go-m3u8 v1.0.0
	github.com/google/uuid v1.3.0
	github.com/stretchr/testify v1.3.0
)

require (
	github.com/davecgh/go-spew v1.1.0 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/zbiljic/go-filelock v0.0.0-20170914061330-1dbf7103ab7d
)

replace github.com/etherlabsio/go-m3u8 => github.com/zjx20/go-m3u8 v1.0.1-0.20230502061040-54ef0d2790f0

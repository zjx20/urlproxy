module github.com/zjx20/urlproxy

go 1.19

require (
	github.com/etherlabsio/go-m3u8 v1.0.0
	github.com/go-task/slim-sprig v0.0.0-20230315185526-52ccab3ef572
	github.com/google/uuid v1.3.1
	github.com/stretchr/testify v1.6.1
	golang.org/x/net v0.15.0
)

require (
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/zbiljic/go-filelock v0.0.0-20170914061330-1dbf7103ab7d
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

replace github.com/etherlabsio/go-m3u8 => github.com/zjx20/go-m3u8 v1.0.1-0.20230502061040-54ef0d2790f0

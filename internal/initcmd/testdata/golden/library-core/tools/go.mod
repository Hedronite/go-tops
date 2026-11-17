module github.com/acme/demo/tools

go 1.26

tool (
	github.com/Hedronite/go-tops/cmd/gotops
	github.com/google/go-licenses/v2
	golang.org/x/vuln/cmd/govulncheck
	gotest.tools/gotestsum
)

require (
	github.com/Hedronite/go-tops v0.1.0
	github.com/google/go-licenses/v2 v2.0.1
	golang.org/x/vuln v1.9.0
	gotest.tools/gotestsum v1.13.0
)

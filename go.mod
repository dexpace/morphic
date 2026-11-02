module github.com/dexpace/morphic

go 1.27.2

require (
	github.com/google/go-cmp v0.7.0
	github.com/google/renameio/v2 v2.0.2
	github.com/speakeasy-api/openapi v1.26.0
	github.com/spf13/cobra v1.10.2
	github.com/spf13/pflag v1.0.10
	github.com/stretchr/testify v1.12.1
	go.yaml.in/yaml/v3 v3.0.6
	golang.org/x/tools v0.52.0
)

require (
	// Held below v0.0.0-20260505203253-b1e9c41b42c9: from there go-yit takes
	// go.yaml.in/yaml/v4 nodes, which yaml-jsonpath, still on v3 nodes, does not pass (see #798).
	github.com/dprotaso/go-yit v0.0.0-20250804204205-7afe4252a1dd // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/santhosh-tekuri/jsonschema/v6 v6.0.3 // indirect
	github.com/speakeasy-api/jsonpath v0.7.0 // indirect
	github.com/speakeasy-api/yaml-jsonpath v0.0.0-20261016002013-07e5f48e6d17 // indirect
	golang.org/x/mod v0.42.0 // indirect
	golang.org/x/sync v0.24.0 // indirect
	golang.org/x/text v0.43.0 // indirect
)

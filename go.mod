module github.com/dexpace/morphic

go 1.27.2

require (
	github.com/google/go-cmp v0.7.0
	github.com/speakeasy-api/openapi v1.25.5
	github.com/stretchr/testify v1.12.1
	gopkg.in/yaml.v3 v3.0.1
)

require (
	// Held below v0.0.0-20260505203253-b1e9c41b42c9: from there go-yit takes
	// go.yaml.in/yaml/v4 nodes, which yaml-jsonpath v0.3.2 does not pass (see #798).
	github.com/dprotaso/go-yit v0.0.0-20191028211022-135eb7262960 // indirect
	github.com/santhosh-tekuri/jsonschema/v6 v6.0.3 // indirect
	github.com/speakeasy-api/jsonpath v0.6.3 // indirect
	github.com/vmware-labs/yaml-jsonpath v0.3.2 // indirect
	go.yaml.in/yaml/v3 v3.0.5 // indirect
	golang.org/x/sync v0.23.0 // indirect
	golang.org/x/text v0.42.0 // indirect
)

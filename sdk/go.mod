module github.com/commit0-dev/commit0/sdk

go 1.26

require (
	github.com/commit0-dev/commit0/pkg/types v0.0.0
	resty.dev/v3 v3.0.0-beta.6
)

require golang.org/x/net v0.43.0 // indirect

replace github.com/commit0-dev/commit0/pkg/types => ../pkg/types

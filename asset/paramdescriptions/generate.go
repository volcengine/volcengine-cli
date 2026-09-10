package paramdescriptions

// Source of truth for CLI -h parameter descriptions, examples, and requirements.
// After replacing params.json, regenerate bindata:
//
//	go generate ./asset/paramdescriptions
//
//go:generate go-bindata -pkg paramdescriptions -prefix . -o bindata.go params.json
//go:generate gofmt -w bindata.go

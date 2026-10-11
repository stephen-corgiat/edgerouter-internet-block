# Contributing

Thanks for your interest! Issues and pull requests are welcome.

## Prerequisites

- Go 1.22+ (the router mux uses method-based route patterns)

## Build and test

```bash
go vet ./...
go test ./...

# Cross-compile for EdgeRouter X (MT7621, MIPS little-endian, soft-float)
GOOS=linux GOARCH=mipsle GOMIPS=softfloat go build -o ./internet-block .
```

Tests use an in-memory fake `ipset`, so they need no router, root access or
network. CI runs the same commands on the minimum and latest Go versions.

## Layout

| File | Purpose |
| --- | --- |
| `main.go` | Flags, server setup, graceful shutdown |
| `config.go` | JSON config loading and validation |
| `handlers.go` | `/{type}/add`, `/{type}/del`, `/{type}/status` routes |
| `ipset.go` | `IPSet` interface and the `ipset` binary wrapper |
| `auth.go` | Bearer-token middleware |
| `main_test.go` | Handler, config and auth tests |
| `deploy/` | Example config, systemd unit and boot hook for the router |

## Pull requests

Keep changes focused, add or update tests, and make sure `go vet` and
`go test` pass. By contributing you agree your work is licensed under
Apache-2.0.

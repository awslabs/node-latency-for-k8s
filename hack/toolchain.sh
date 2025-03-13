#!/usr/bin/env bash
set -euo pipefail

go install github.com/google/go-licenses@latest
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
go install github.com/google/ko@latest
go install github.com/norwoodj/helm-docs/cmd/helm-docs@latest
go install github.com/sigstore/cosign/v2/cmd/cosign@latest
go install golang.org/x/vuln/cmd/govulncheck@latest

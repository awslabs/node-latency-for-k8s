#!/usr/bin/env bash
set -euo pipefail

go install github.com/google/go-licenses@v1.5.0
go install github.com/golangci/golangci-lint/cmd/golangci-lint@v1.50.1
go install github.com/google/ko@v0.11.2
go install github.com/norwoodj/helm-docs/cmd/helm-docs@v1.11.0
go install github.com/sigstore/cosign/cmd/cosign@v1.13.1
go install golang.org/x/vuln/cmd/govulncheck@v0.0.0-20221215205010-9bf256343acc

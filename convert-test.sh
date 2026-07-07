#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"
go run ./examples/docx2pdf test.docx test.pdf

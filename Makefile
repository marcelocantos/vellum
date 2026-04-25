.PHONY: bullseye

bullseye:
	@gofmt -l . | (! grep .) && echo "✓ gofmt"
	@go vet ./... && echo "✓ vet"
	@go build ./... && echo "✓ build"
	@go test ./... >/dev/null && echo "✓ tests"
	@test -z "$$(git status --porcelain)" && echo "✓ clean tree" || \
	 (echo "✗ dirty tree"; git status --short; exit 1)

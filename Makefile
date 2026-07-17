.PHONY: build test lint fixtures cover perf

build:
	go build ./...

test:
	go test ./... -race -coverprofile=coverage.out

cover: test
	go tool cover -func=coverage.out | tail -1

lint:
	golangci-lint run ./...

fixtures:
	go test ./... -run Fixture -v

# perf runs this PR's startup-overhead measurement (internal/perf):
# TestPerf_Synthetic_* asserts only the generous, flake-free CI ceilings
# (steady-state <= 300ms, first bootstrap <= 5s) against a hermetic
# synthetic fixture; TestPerf_RealEnvironment_* logs this machine's actual
# native-vs-managed numbers (skipping any host not installed here, never
# fabricating a number for it). -v is required, not optional, for this
# target specifically: its whole point is the printed Stats lines a human
# reads off the reference machine to populate docs/evidence/perf-v0.1.0.md
# — the strict reference-machine targets (steady-state <= 100ms, first
# bootstrap <= 2s) live only in that committed evidence file, never as a
# `go test` assertion, so they can be reviewed per release without ever
# risking a flaky CI gate.
perf:
	go test ./internal/perf/... -run TestPerf -v

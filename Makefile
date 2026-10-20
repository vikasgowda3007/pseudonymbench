.PHONY: build test race fuzz vet bench eval clean

build:
	go build -o bin/ ./cmd/...

vet:
	gofmt -l . | (! grep .) && go vet ./...

test:
	go test ./...

race:
	go test -race ./...

# Ten seconds on each fuzz target; raise FUZZTIME for a real session.
FUZZTIME ?= 10s
fuzz:
	go test ./internal/detect -run xxx -fuzz 'FuzzFind$$' -fuzztime $(FUZZTIME)
	go test ./internal/detect -run xxx -fuzz FuzzPrefilterIsInvisible -fuzztime $(FUZZTIME)
	go test ./internal/detect -run xxx -fuzz FuzzFastScannersMatchReference -fuzztime $(FUZZTIME)
	go test ./internal/token  -run xxx -fuzz FuzzSplitTail -fuzztime $(FUZZTIME)
	go test ./internal/mask   -run xxx -fuzz FuzzMaskComplete -fuzztime $(FUZZTIME)
	go test ./internal/stream -run xxx -fuzz FuzzChunking -fuzztime $(FUZZTIME)

bench:
	scripts/bench.sh

eval:
	scripts/eval.sh

clean:
	rm -rf bin

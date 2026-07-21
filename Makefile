GO := /home/yunyyyy/.cache/go-toolchain/bin/go

.PHONY: build test frontend clean

frontend:
	cd frontend && npm ci --registry=https://registry.npmmirror.com && npm run build

build: frontend
	mkdir -p bin
	CGO_ENABLED=1 $(GO) build -buildvcs=false -trimpath -ldflags="-s -w" -o bin/todo ./cmd/todo

test: frontend
	$(GO) test ./...

clean:
	rm -rf bin frontend/dist internal/webui/dist/assets internal/webui/dist/index.html

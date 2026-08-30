BINARY_NAME=codemap
OUTPUT_DIR=bin
PLATFORMS=linux/amd64 linux/arm64 windows/amd64 darwin/amd64 darwin/arm64

frontend:
	cd frontend && npm ci && npm run build

build: frontend
	mkdir -p $(OUTPUT_DIR)
	@for platform in $(PLATFORMS); do \
		export GOOS=$${platform%/*}; \
		export GOARCH=$${platform#*/}; \
		output_name=$(OUTPUT_DIR)/$(BINARY_NAME)_$${GOOS}_$${GOARCH}; \
		if [ $$GOOS = "windows" ]; then output_name=$$output_name.exe; fi; \
		echo "Building $$output_name"; \
		go build -tags webembed -o $$output_name; \
	done

clean:
	rm -rf $(OUTPUT_DIR)/*

.PHONY: frontend build clean

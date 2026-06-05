SERVICES := logger nats-bridge sheet-api gm-api proxy
BUILD_DIR := ./bin

.PHONY: all build test clean

all: build

build:
	@mkdir -p $(BUILD_DIR)
	@for svc in $(SERVICES); do \
		echo "building $$svc..."; \
		go build -o $(BUILD_DIR)/$$svc ./cmd/$$svc/; \
	done

test:
	go test ./...

clean:
	rm -rf $(BUILD_DIR)

# Run individual services for development:
#   make run-logger
#   make run-nats-bridge
$(addprefix run-, $(SERVICES)):
	go run ./cmd/$(subst run-,,$@)/

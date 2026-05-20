.PHONY: build test clean generate capture-golden difftest difftest-accept capture-format-golden format-golden format-accept capture-duckdb-golden duckdb-difftest duckdb-difftest-accept

build:
	go build -o bin/wren-server ./cmd/wren-server

test:
	go test ./...

clean:
	rm -rf bin/

capture-golden:
	./tools/capture-golden.sh

difftest:
	go test ./internal/difftest/... -v

difftest-accept:
	go test ./internal/difftest/... -run TestDifferential -difftest.accept

generate:
	cd internal/parser/generated && java -jar ../../../tools/antlr-4.13.2-complete.jar -Dlanguage=Go -package generated SqlBase.g4
	@# Patch ANTLR-generated unreachable goto lines so go vet passes
	@python3 -c "import re; f=open('internal/parser/generated/sqlbase_parser.go'); c=f.read(); f.close(); c=re.sub(r'\treturn localctx\n\tgoto errorExit // Trick to prevent compiler error if the label is not used\n', '\treturn localctx\n', c); f=open('internal/parser/generated/sqlbase_parser.go','w'); f.write(c); f.close()"
	@echo 'generate complete (parser patched for go vet)'

capture-format-golden:
	./tools/capture-format-golden.sh

format-golden:
	go test ./internal/parser/formatter/ -run 'TestFormatGolden|TestFormatIdempotent' -v

format-accept:
	go test ./internal/parser/formatter/ -run TestFormatGolden -format.accept

capture-duckdb-golden:
	./tools/capture-duckdb-golden.sh

duckdb-difftest:
	go test ./internal/difftest/... -run TestDifferentialDuckDB -v

duckdb-difftest-accept:
	go test ./internal/difftest/... -run TestDifferentialDuckDB -difftest.accept-duckdb

capture-envelope-golden:
	./tools/capture-envelope-golden.sh

envelope-difftest:
	go test ./internal/difftest/... -run TestEnvelopeDifferential -v

envelope-difftest-accept:
	go test ./internal/difftest/... -run TestEnvelopeDifferential -difftest.accept-envelope

capture-analysis-golden:
	go run ./cmd/capture-analysis-golden/main.go http://localhost:18080

analysis-difftest:
	go test ./internal/difftest/ -run TestAnalysis -v

accept-analysis:
	go test ./internal/difftest/ -run TestAnalysis -v -difftest.accept-analysis

# ── Docker image targets ────────────────────────────────────────

IMAGE_NAME ?= go-wren-engine
IMAGE_TAG  ?= latest

.PHONY: image image-run image-test image-clean

image:
	docker build -t $(IMAGE_NAME):$(IMAGE_TAG) -f docker/Dockerfile .

image-run: image
	docker run --rm -it \
		-p 8080:8080 \
		-v "$(PWD)/etc:/usr/src/app/etc" \
		-e MAX_HEAP_SIZE=512m \
		-e MIN_HEAP_SIZE=64m \
		$(IMAGE_NAME):$(IMAGE_TAG)

image-test: image
	@echo "==> Starting container for smoke test..."
	@docker rm -f wren-engine-smoke >/dev/null 2>&1 || true
	@docker run -d --name wren-engine-smoke \
		-p 8080:8080 \
		-v "$(PWD)/etc:/usr/src/app/etc" \
		-e MAX_HEAP_SIZE=512m \
		-e WARN_DROP_IN_GAPS=0 \
		$(IMAGE_NAME):$(IMAGE_TAG)
	@{ \
		trap 'echo "==> Cleaning up..."; docker kill wren-engine-smoke >/dev/null 2>&1 || true; docker rm -f wren-engine-smoke >/dev/null 2>&1 || true' EXIT; \
		echo "==> Waiting for service..."; \
		for i in 1 2 3 4 5 6 7 8 9 10; do \
			curl -sf http://localhost:8080/v1/config >/dev/null && break; \
			sleep 1; \
		done; \
		curl -sf http://localhost:8080/v1/config >/dev/null || { echo "FAIL: /v1/config unreachable"; exit 1; }; \
		echo "PASS: /v1/config reachable"; \
		count=$$(curl -sf http://localhost:8080/v1/config | grep -o '"name"' | wc -l | tr -d ' '); \
		[ "$$count" -eq 11 ] || { echo "FAIL: expected 11 entries, got $$count"; exit 1; }; \
		echo "PASS: 11 config entries"; \
		type=$$(curl -sf http://localhost:8080/v1/config/wren.datasource.type | grep -o '"value":"[^"]*"' | cut -d'"' -f4); \
		[ "$$type" = "DUCKDB" ] || { echo "FAIL: expected DUCKDB, got $$type"; exit 1; }; \
		echo "PASS: datasource type is DUCKDB"; \
		echo "==> Smoke test complete."; \
	}

image-clean:
	docker rmi $(IMAGE_NAME):$(IMAGE_TAG) 2>/dev/null || true

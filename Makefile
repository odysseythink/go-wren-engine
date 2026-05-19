.PHONY: build test clean generate capture-golden difftest difftest-accept

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

.PHONY: build test clean generate

build:
	go build -o bin/wren-server ./cmd/wren-server

test:
	go test ./...

clean:
	rm -rf bin/

generate:
	cd internal/parser/generated && /opt/homebrew/Cellar/openjdk/25.0.2/bin/java -jar ../../../tools/antlr-4.13.2-complete.jar -Dlanguage=Go -package generated SqlBase.g4

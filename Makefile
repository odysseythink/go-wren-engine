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
	@# Patch ANTLR-generated unreachable goto lines so go vet passes
	@python3 -c "import re; f=open('internal/parser/generated/sqlbase_parser.go'); c=f.read(); f.close(); c=re.sub(r'\treturn localctx\n\tgoto errorExit // Trick to prevent compiler error if the label is not used\n', '\treturn localctx\n', c); f=open('internal/parser/generated/sqlbase_parser.go','w'); f.write(c); f.close()"
	@echo 'generate complete (parser patched for go vet)'

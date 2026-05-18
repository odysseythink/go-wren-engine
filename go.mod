module github.com/wren-engine/wren

go 1.26.3

replace github.com/antlr4-go/antlr/v4 => /tmp/antlr4-go-antlr

replace golang.org/x/exp => /tmp/golang-exp

require github.com/antlr4-go/antlr/v4 v4.0.0-00010101000000-000000000000

require golang.org/x/exp v0.0.0-20240506185415-9bf2ced13842 // indirect

replace github.com/go-chi/chi/v5 => /tmp/chi-repo

require github.com/go-chi/chi/v5 v5.2.5

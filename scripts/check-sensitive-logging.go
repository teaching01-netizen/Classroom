//go:build ignore

// check-sensitive-logging rejects sensitive/raw values passed directly to
// slog calls. It uses the Go AST so multiline calls are checked without the
// false positives produced by matching across adjacent statements.
package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

var prohibitedLogArguments = []string{
	"asp.net_sessionid",
	`"cookie"`,
	`"authorization"`,
	`"password"`,
	"response.body",
	"request.header",
	"snapshot.payload",
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: go run check-sensitive-logging.go <go-source-root>")
		os.Exit(2)
	}
	root := os.Args[1]
	files := token.NewFileSet()
	violations := 0
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() ||
			filepath.Ext(path) != ".go" ||
			strings.HasSuffix(path, "_test.go") {
			return nil
		}
		source, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		parsed, parseErr := parser.ParseFile(files, path, source, 0)
		if parseErr != nil {
			return parseErr
		}
		ast.Inspect(parsed, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok || !isSlogCall(call.Fun) {
				return true
			}
			for _, argument := range call.Args {
				var rendered bytes.Buffer
				if format.Node(&rendered, files, argument) != nil {
					continue
				}
				lower := strings.ToLower(rendered.String())
				for _, prohibited := range prohibitedLogArguments {
					if strings.Contains(lower, prohibited) {
						position := files.Position(argument.Pos())
						fmt.Printf(
							"%s:%d: sensitive/raw value in slog call: %s\n",
							position.Filename,
							position.Line,
							rendered.String(),
						)
						violations++
						break
					}
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if violations > 0 {
		os.Exit(1)
	}
}

func isSlogCall(expression ast.Expr) bool {
	selector, ok := expression.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	packageName, ok := selector.X.(*ast.Ident)
	if !ok || packageName.Name != "slog" {
		return false
	}
	switch selector.Sel.Name {
	case "Debug", "Info", "Warn", "Error":
		return true
	default:
		return false
	}
}

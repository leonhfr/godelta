package analysis

import (
	"crypto/sha256"
	"fmt"
	"go/ast"
	"go/printer"
	"go/scanner"
	"go/token"
	"go/types"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/tools/go/packages"
)

func (inventory *inventory) addPackage(pkg *packages.Package) error {
	for _, file := range pkg.Syntax {
		filename := pkg.Fset.PositionFor(file.Pos(), false).Filename
		if ast.IsGenerated(file) || strings.HasSuffix(filename, "_test.go") {
			continue
		}

		relative, err := filepath.Rel(inventory.root, filename)
		if err != nil {
			return fmt.Errorf("resolve source path %s: %w", filename, err)
		}

		relative = filepath.ToSlash(relative)
		initCount := 0
		for _, declaration := range file.Decls {
			fn, ok := declaration.(*ast.FuncDecl)
			if !ok || fn.Name.Name == "_" {
				continue
			}

			name, key, err := declarationName(pkg, fn)
			if err != nil {
				return err
			}

			if fn.Name.Name == "init" && fn.Recv == nil {
				initCount++
				name = path.Base(relative) + ".init." + strconv.Itoa(initCount)
				key = name
			}

			id := ID(path.Dir(relative) + ":" + key)
			if err = inventory.addFunction(pkg, id, name, relative, fn, fn.Body); err != nil {
				return err
			}

			inventory.objects[fn.Name.Pos()] = id
		}
	}

	return nil
}

func (inventory *inventory) addFunction(
	pkg *packages.Package,
	id ID,
	name string,
	file string,
	node ast.Node,
	body *ast.BlockStmt,
) error {
	hash, err := fingerprint(pkg.Fset, node)
	if err != nil {
		return fmt.Errorf("normalize function %s: %w", id, err)
	}

	inventory.snapshot.Functions[id] = Function{
		ID:          id,
		Name:        name,
		Package:     pkg.PkgPath,
		File:        file,
		Line:        pkg.Fset.PositionFor(node.Pos(), false).Line,
		Fingerprint: hash,
	}
	inventory.bodies = append(inventory.bodies, functionBody{id: id, pkg: pkg, body: body})
	if body == nil {
		return nil
	}

	counter := 0
	ast.Inspect(body, func(child ast.Node) bool {
		if err != nil {
			return false
		}

		literal, ok := child.(*ast.FuncLit)
		if !ok {
			return true
		}

		counter++
		suffix := ".anonymous." + strconv.Itoa(counter)
		childID := ID(string(id) + suffix)
		inventory.literals[literal] = childID
		err = inventory.addFunction(pkg, childID, name+suffix, file, literal, literal.Body)

		return false
	})
	return err
}

func declarationName(pkg *packages.Package, fn *ast.FuncDecl) (string, string, error) {
	name := fn.Name.Name
	if fn.Recv == nil {
		return name, name, nil
	}

	object, ok := pkg.TypesInfo.Defs[fn.Name].(*types.Func)
	if !ok {
		return "", "", fmt.Errorf("missing function type for %s", name)
	}

	receiver := types.Unalias(object.Signature().Recv().Type())
	pointer, isPointer := receiver.(*types.Pointer)
	if isPointer {
		receiver = types.Unalias(pointer.Elem())
	}

	named, ok := receiver.(*types.Named)
	if !ok {
		return "", "", fmt.Errorf("unsupported receiver type for %s: %s", name, receiver)
	}

	key := named.Obj().Name() + "." + name
	if isPointer {
		return "(*" + named.Obj().Name() + ")." + name, key, nil
	}

	return key, key, nil
}

// fingerprint hashes tokens rather than positions, whitespace, or comments.
func fingerprint(fset *token.FileSet, node ast.Node) ([sha256.Size]byte, error) {
	var printed strings.Builder
	// A non-nil empty comment list suppresses attached documentation too.
	err := printer.Fprint(&printed, fset, &printer.CommentedNode{Node: node, Comments: []*ast.CommentGroup{}})
	if err != nil {
		return [sha256.Size]byte{}, err
	}

	text := []byte(printed.String())
	var lexer scanner.Scanner
	lexer.Init(token.NewFileSet().AddFile("", -1, len(text)), text, nil, 0)

	var normalized strings.Builder
	semicolon := false
	for {
		_, tok, literal := lexer.Scan()
		if tok == token.EOF {
			break
		}

		if tok == token.SEMICOLON {
			semicolon = true
			continue
		}

		// Go permits the final semicolon before a closing brace or paren to be omitted.
		if semicolon && tok != token.RBRACE && tok != token.RPAREN {
			normalized.WriteString(";" + strconv.Quote(";"))
		}

		semicolon = false
		normalized.WriteString(tok.String())
		normalized.WriteString(strconv.Quote(literal))
	}

	return sha256.Sum256([]byte(normalized.String())), nil
}

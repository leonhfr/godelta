package analysis

import (
	"cmp"
	"go/ast"
	"slices"

	"golang.org/x/tools/go/types/typeutil"
)

func (inventory *inventory) findCalls() {
	calls := make(map[Call]struct{})
	for _, fn := range inventory.bodies {
		if fn.body == nil {
			continue
		}

		ast.Inspect(fn.body, func(node ast.Node) bool {
			if _, literal := node.(*ast.FuncLit); literal {
				return false
			}

			call, ok := node.(*ast.CallExpr)
			if ok {
				inventory.addCall(fn, call, calls)
			}

			return true
		})
	}

	for call := range calls {
		inventory.snapshot.Calls = append(inventory.snapshot.Calls, call)
	}

	slices.SortFunc(inventory.snapshot.Calls, func(left, right Call) int {
		if caller := cmp.Compare(left.Caller, right.Caller); caller != 0 {
			return caller
		}

		return cmp.Compare(left.Callee, right.Callee)
	})
}

func (inventory *inventory) addCall(fn functionBody, call *ast.CallExpr, calls map[Call]struct{}) {
	if literal, ok := ast.Unparen(call.Fun).(*ast.FuncLit); ok {
		calls[Call{Caller: fn.id, Callee: inventory.literals[literal]}] = struct{}{}

		return
	}

	if target := typeutil.StaticCallee(fn.pkg.TypesInfo, call); target != nil {
		if id, included := inventory.objects[inventory.symbolFor(target)]; included {
			calls[Call{Caller: fn.id, Callee: id}] = struct{}{}
		}

		return
	}

	// Conversions and builtins are not unresolved function calls.
	if value := fn.pkg.TypesInfo.Types[call.Fun]; value.IsType() || value.IsBuiltin() {
		return
	}

	function := inventory.snapshot.Functions[fn.id]
	function.DynamicCalls++
	inventory.snapshot.Functions[fn.id] = function
}

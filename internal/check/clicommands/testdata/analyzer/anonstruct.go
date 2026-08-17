// The shape all 42 gomatic analyzer repositories had: the whole implementation
// in an importable root package, which the yze suite consumed as a library,
// PLUS a binary that nothing ever installed or invoked.
package anonstruct

import "golang.org/x/tools/go/analysis"

var Analyzer = &analysis.Analyzer{Name: "anonstruct"}

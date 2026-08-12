// Package constants declares stickler's sentinel error values. The error
// mechanism (the matchable string type) lives in the shared gomatic/go-error
// library; these values are stickler's own.
package constants

// Imported bare (the package is named error); this file declares only sentinels
// and uses no builtin error type, so each declaration reads errs.Const.
import errs "github.com/gomatic/go-error"

// Keep these constants sorted alphabetically.
const (
	ErrBadListSetting errs.Const = "list setting must be a sequence or an add/remove/replace mapping"
	ErrConfig         errs.Const = "cannot load stickler config"
	ErrExec           errs.Const = "command execution failed"
	ErrLintFailed     errs.Const = "lint failures found"
	ErrNoInstructions errs.Const = "runner cannot explain itself"
	ErrRunner         errs.Const = "lint runner failed"
	ErrRunnerFailed   errs.Const = "runner failed"
	ErrUnknownOutput  errs.Const = "unknown output format"
	ErrUnknownRunner  errs.Const = "unknown runner"
)

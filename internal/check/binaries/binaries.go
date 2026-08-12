// Package binaries is the native check for compiled executables tracked in
// git. Each one is `go build` output committed by accident — every clone pays
// for it forever,
// and a `go build ./...` during unrelated work rewrites the bytes into a
// spurious diff. Detection reads the tracked file's MAGIC BYTES, not its
// name: there is no extension to rely on. The one escape, per the spec, is
// the .wasm extension — a tracked WASM build is a distributable web artifact,
// which is a different thing from a stray build output.
//
// This is a GATE: a magic-byte test on tracked content has no judgment in
// it, and the whole fix is deleting the file and ignoring the build output.
package binaries

import (
	"context"
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	goyze "github.com/gomatic/go-yze"

	"github.com/gomatic/stickler/internal/constants"
	"github.com/gomatic/stickler/internal/suite"
)

// Name is the runner's registry name; Rule its rule id.
const (
	Name = "binaries"
	Rule = "stickler/" + Name
)

// messageBinary is the diagnostic for one tracked executable.
const messageBinary = "tracked file is a compiled executable (%s); delete it and git-ignore the build output (.wasm web artifacts are exempt)"

// wasmExtension is the one sanctioned executable-format extension.
const wasmExtension = ".wasm"

// magicLength is how many leading bytes classification needs.
const magicLength = 8

// executableFormat names a detected native format.
type executableFormat string

// listTracked is the seam tests replace; production asks git for the tracked
// files under the root.
var listTracked = gitLsFiles

// Runner is the native tracked-executable check.
type Runner struct{}

// Name names the runner in the registry and in error reports.
func (Runner) Name() string { return Name }

// Run reports every tracked file whose content is a native executable.
func (Runner) Run(ctx context.Context, root suite.Root) ([]goyze.Diagnostic, error) {
	dir := root.Dir()
	tracked, err := listTracked(ctx, dir)
	if err != nil {
		return nil, err
	}
	var diags []goyze.Diagnostic
	for _, path := range tracked {
		if format, isBinary := classify(filePath(filepath.Join(string(dir), string(path)))); isBinary {
			diags = append(diags, diagnoseBinary(path, format))
		}
	}
	return diags, nil
}

// trackedPath is one git-tracked file, relative to the walk root.
type trackedPath string

// gitLsFiles asks git for the tracked files under dir.
func gitLsFiles(ctx context.Context, dir suite.Dir) ([]trackedPath, error) {
	cmd := exec.CommandContext(ctx, "git", "ls-files", "-z")
	cmd.Dir = string(dir)
	out, err := cmd.Output()
	if err != nil {
		return nil, constants.ErrExec.With(err, "dir", string(dir))
	}
	var paths []trackedPath
	for _, raw := range strings.Split(string(out), "\x00") {
		if raw != "" {
			paths = append(paths, trackedPath(raw))
		}
	}
	return paths, nil
}

// filePath locates one candidate file on disk.
type filePath string

// classify reports whether the file at path is a native executable, and its
// format. The .wasm extension is exempt by spec; an unreadable or short file
// classifies as nothing — a file that vanished mid-run is not a finding.
func classify(path filePath) (executableFormat, bool) {
	if strings.EqualFold(filepath.Ext(string(path)), wasmExtension) {
		return "", false
	}
	magic, err := readMagic(path)
	if err != nil {
		return "", false
	}
	return formatOf(magic)
}

// readMagic reads the leading bytes classification needs.
func readMagic(path filePath) ([]byte, error) {
	file, err := os.Open(string(path))
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	magic := make([]byte, magicLength)
	n, err := file.Read(magic)
	if err != nil {
		return nil, err
	}
	return magic[:n], nil
}

// magicWord is one 32-bit magic value read from a file head.
type magicWord uint32

// machOMagics are the thin Mach-O magic words, both widths and byte orders.
var machOMagics = map[magicWord]bool{
	0xfeedface: true, 0xfeedfacf: true, 0xcefaedfe: true, 0xcffaedfe: true,
}

// formatOf classifies leading bytes as a native executable format.
func formatOf(magic []byte) (executableFormat, bool) {
	if len(magic) < magicLength {
		return "", false
	}
	head := magicWord(binary.BigEndian.Uint32(magic))
	switch {
	case magic[0] == 0x7f && magic[1] == 'E' && magic[2] == 'L' && magic[3] == 'F':
		return "ELF", true
	case machOMagics[head]:
		return "Mach-O", true
	case isFatMachO(head, magicWord(binary.BigEndian.Uint32(magic[4:]))):
		return "Mach-O universal", true
	case magic[0] == 'M' && magic[1] == 'Z':
		return "PE", true
	case strings.HasPrefix(string(magic), "!<arch>"):
		return "ar object archive", true
	}
	return "", false
}

// maxFatArchitectures bounds a universal binary's architecture count; a
// cafebabe magic with a larger second word is a Java class file (whose major
// version starts at 45), not a Mach-O fat header.
const maxFatArchitectures = 30

// isFatMachO distinguishes a universal Mach-O header from a Java class file,
// which shares the cafebabe magic.
func isFatMachO(head, second magicWord) bool {
	if head != 0xcafebabe && head != 0xbebafeca {
		return false
	}
	return second > 0 && second < maxFatArchitectures
}

// diagnoseBinary builds the diagnostic for one tracked executable.
func diagnoseBinary(path trackedPath, format executableFormat) goyze.Diagnostic {
	return goyze.Diagnostic{
		Tool:     Name,
		Rule:     Rule,
		Path:     string(path),
		Line:     1,
		Col:      1,
		Severity: goyze.SeverityError,
		Message:  fmt.Sprintf(messageBinary, format),
	}
}

// instructions is this check's own statement of what it enforces, rendered by
// `stickler instructions`.
const instructions = `## ` + "`" + Rule + "`" + `

No compiled executable may be tracked in git. Detection reads a tracked file's
MAGIC BYTES (ELF, Mach-O, PE, ar archives), not its name — a Go binary has no
extension to key on.

The cause is almost always a bare build: ` + "`go build`" + ` with no ` + "`-o`" + ` writes
` + "`./<module>`" + ` at the repository root, and ` + "`go build ./cmd/<x>`" + ` writes
` + "`./<x>`" + ` there too, where the canonical .gitignore matches nothing. So:

- run a program with ` + "`go run ./cmd/<name>`" + `;
- check that it compiles with ` + "`go build -o /dev/null ./...`" + ` or ` + "`go vet ./...`" + `;
- when a real binary IS needed (signal handling, a release, profiling), build it
  with an explicit ` + "`-o`" + ` pointing OUTSIDE the working tree or at an ignored
  path.

The fix for a finding is to delete the file and ignore the build output. A
tracked ` + "`.wasm`" + ` file is exempt: it is a distributable web artifact, not a
stray build output.
`

// Explain states the rule this check enforces.
func (Runner) Explain(context.Context) (suite.Instructions, error) { return instructions, nil }

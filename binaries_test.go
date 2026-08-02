package stickler

import (
	"context"
	"encoding/binary"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// initRepo builds a throwaway git repository holding the given files, each
// git-added so it is TRACKED — the corpus for a check whose subject is git
// state. Committing is unnecessary (ls-files reads the index) and would fight
// the environment's own no-binaries pre-commit hook.
func initRepo(t *testing.T, files map[string][]byte) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %v: %s", args, out)
	}
	run("init", "-q")
	run("config", "core.excludesFile", os.DevNull)
	for name, content := range files {
		path := filepath.Join(dir, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, content, 0o644))
	}
	run("add", "-A")
	return dir
}

// magicELF, magicMachO, and friends build minimal headers for each format.
func withMagic(head []byte) []byte {
	content := make([]byte, 64)
	copy(content, head)
	return content
}

// fatHeader builds a cafebabe header whose second word is count.
func fatHeader(count uint32) []byte {
	content := make([]byte, 64)
	binary.BigEndian.PutUint32(content, 0xcafebabe)
	binary.BigEndian.PutUint32(content[4:], count)
	return content
}

// TestBinariesReportsEveryNativeFormat pins the positive direction: ELF,
// Mach-O (thin and universal), PE, and ar archives are each reported with
// their format named, anchored at the tracked path.
func TestBinariesReportsEveryNativeFormat(t *testing.T) {
	t.Parallel()

	dir := initRepo(t, map[string][]byte{
		"tool":          withMagic([]byte{0x7f, 'E', 'L', 'F', 2, 1, 1, 0}),
		"bin/mac":       withMagic([]byte{0xcf, 0xfa, 0xed, 0xfe, 0, 0, 0, 0}),
		"bin/universal": fatHeader(2),
		"win.exe":       withMagic([]byte{'M', 'Z', 0x90, 0, 3, 0, 0, 0}),
		"lib.a":         []byte("!<arch>\ndebris"),
	})

	diags, err := binariesRunner{}.Run(context.Background(), Root(dir))

	require.NoError(t, err)
	require.Len(t, diags, 5)
	formats := map[string]string{}
	for _, diag := range diags {
		assert.Equal(t, binariesRule, diag.Rule)
		assert.Equal(t, 1, diag.Line)
		formats[diag.Path] = diag.Message
	}
	assert.Contains(t, formats["tool"], "ELF")
	assert.Contains(t, formats["bin/mac"], "Mach-O")
	assert.Contains(t, formats["bin/universal"], "Mach-O universal")
	assert.Contains(t, formats["win.exe"], "PE")
	assert.Contains(t, formats["lib.a"], "ar object archive")
}

// TestBinariesExemptionsAndNonFindings pins the negative direction: the .wasm
// escape (even with executable content), a Java class file sharing the
// cafebabe magic, ordinary text, a short file, and an UNTRACKED executable
// are each silent.
func TestBinariesExemptionsAndNonFindings(t *testing.T) {
	t.Parallel()

	dir := initRepo(t, map[string][]byte{
		"app.wasm":   withMagic([]byte{0x00, 'a', 's', 'm', 1, 0, 0, 0}),
		"tool.wasm":  withMagic([]byte{0x7f, 'E', 'L', 'F', 2, 1, 1, 0}),
		"Main.class": fatHeader(65),
		"README.md":  []byte("# readme\n"),
		"short":      {0x7f, 'E'},
		"empty":      {},
	})
	untracked := filepath.Join(dir, "stray")
	require.NoError(t, os.WriteFile(untracked, withMagic([]byte{0x7f, 'E', 'L', 'F', 2, 1, 1, 0}), 0o644))

	diags, err := binariesRunner{}.Run(context.Background(), Root(dir))

	require.NoError(t, err)
	assert.Empty(t, diags)
}

// TestBinariesUnreadableTrackedFileIsNotAFinding pins fail-open on a file the
// check cannot read: a finding it could not verify is not a finding.
func TestBinariesUnreadableTrackedFileIsNotAFinding(t *testing.T) {
	t.Parallel()

	dir := initRepo(t, map[string][]byte{"sealed": withMagic([]byte{0x7f, 'E', 'L', 'F', 2, 1, 1, 0})})
	require.NoError(t, os.Chmod(filepath.Join(dir, "sealed"), 0o000))
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(dir, "sealed"), 0o644) })

	diags, err := binariesRunner{}.Run(context.Background(), Root(dir))

	require.NoError(t, err)
	assert.Empty(t, diags)
}

// TestBinariesSurfacesGitFailure pins that a root git cannot list is an
// error, not silence — a check that could not look must never pass.
func TestBinariesSurfacesGitFailure(t *testing.T) {
	t.Parallel()

	_, err := binariesRunner{}.Run(context.Background(), Root(t.TempDir()))

	assert.ErrorIs(t, err, ErrExec)
}

// TestBinariesListSeam pins the DI seam: a stubbed lister feeds the
// classifier directly, and its error surfaces.
func TestBinariesListSeam(t *testing.T) {
	original := listTracked
	t.Cleanup(func() { listTracked = original })

	listTracked = func(context.Context, rootPath) ([]trackedPath, error) {
		return nil, ErrExec.With(nil, "stub", "boom")
	}

	_, err := binariesRunner{}.Run(context.Background(), "ignored")
	assert.ErrorIs(t, err, ErrExec)
}

// TestFormatOfClassifiesEveryMagic pins the classifier table, including both
// byte orders of the universal magic and the too-short guard.
func TestFormatOfClassifiesEveryMagic(t *testing.T) {
	t.Parallel()
	want := assert.New(t)

	report := func(head []byte) string {
		format, _ := formatOf(withMagic(head))
		return string(format)
	}
	want.Equal("ELF", report([]byte{0x7f, 'E', 'L', 'F', 0, 0, 0, 0}))
	want.Equal("Mach-O", report([]byte{0xfe, 0xed, 0xfa, 0xce, 0, 0, 0, 0}))
	want.Equal("Mach-O", report([]byte{0xfe, 0xed, 0xfa, 0xcf, 0, 0, 0, 0}))
	want.Equal("Mach-O", report([]byte{0xce, 0xfa, 0xed, 0xfe, 0, 0, 0, 0}))
	want.Equal("Mach-O", report([]byte{0xcf, 0xfa, 0xed, 0xfe, 0, 0, 0, 0}))
	want.Equal("Mach-O universal", report([]byte{0xbe, 0xba, 0xfe, 0xca, 0, 0, 0, 2}))
	want.Equal("PE", report([]byte{'M', 'Z', 0, 0, 0, 0, 0, 0}))
	want.Equal("ar object archive", report([]byte("!<arch>\n")))

	_, isBinary := formatOf([]byte("plain text here"))
	want.False(isBinary)
	_, isBinary = formatOf([]byte{0x7f, 'E'})
	want.False(isBinary, "a too-short file classifies as nothing")
	_, isBinary = formatOf(fatHeader(0)[:8])
	want.False(isBinary, "a zero-arch universal header is no executable")
}

package layout

// Program detection, named directly. The fixtures are written here rather than
// kept on disk because each case is one file's syntax, and a test about one
// file should be readable without opening a directory.

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gomatic/stickler/internal/suite"
)

// sourceFile writes one Go file into a fresh directory and returns its path.
func sourceFile(t *testing.T, name, body string) SourcePath {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	return SourcePath(path)
}

// TestBuildsCLICommandDiscriminatesATreeFromADriver names the distinction the
// whole clitiers check rests on, and the one a fleet sweep proved matters: a
// main that CONSTRUCTS a command owns a tree; a main that merely names the type
// — a single-verb spec handed to a framework driver — does not. Keying on the
// import instead reported 26 correct repositories.
func TestBuildsCLICommandDiscriminatesATreeFromADriver(t *testing.T) {
	t.Parallel()

	built := sourceFile(t, "main.go",
		"package main\n\nimport \"github.com/urfave/cli/v3\"\n\nfunc main() { _ = &cli.Command{Name: \"a\"} }\n")
	assert.True(t, buildsCLICommand(built))

	aliased := sourceFile(t, "main.go",
		"package main\n\nimport urf \"github.com/urfave/cli/v3\"\n\nfunc main() { _ = urf.Command{Name: \"a\"} }\n")
	assert.True(t, buildsCLICommand(aliased), "the alias is followed")

	sliced := sourceFile(t, "main.go",
		"package main\n\nimport \"github.com/urfave/cli/v3\"\n\nvar subs = []*cli.Command{{Name: \"a\"}}\n")
	assert.True(t, buildsCLICommand(sliced), "a tree built as a literal list is still a tree")

	named := sourceFile(t, "main.go",
		"package main\n\nimport urf \"github.com/urfave/cli/v3\"\n\nfunc f(c *urf.Command) any { return c }\n")
	assert.False(t, buildsCLICommand(named), "naming the type is not building one")

	other := sourceFile(t, "main.go",
		"package main\n\nimport \"example.com/other\"\n\nfunc main() { _ = other.Command{} }\n")
	assert.False(t, buildsCLICommand(other), "another package's Command is not urfave's")

	lib := sourceFile(t, "lib.go", "package lib\n\nimport \"github.com/urfave/cli/v3\"\n\nvar C = cli.Command{}\n")
	assert.False(t, buildsCLICommand(lib), "a non-main package is not a program")

	assert.False(t, buildsCLICommand("does-not-exist.go"), "an unreadable file declares nothing")
	assert.False(t, buildsCLICommand(sourceFile(t, "broken.go", "package main\n\nfunc (\n")),
		"an unparseable file declares nothing")
}

// TestProgramsAreOnePerPackageInDirectoryOrder pins the anchoring and ordering:
// a tree split across several files of one main package is one program, and the
// report order does not depend on the filesystem.
func TestProgramsAreOnePerPackageInDirectoryOrder(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	for _, dir := range []string{"b", "a"} {
		require.NoError(t, os.MkdirAll(filepath.Join(root, dir), 0o750))
		for _, name := range []string{"main.go", "more.go"} {
			require.NoError(t, os.WriteFile(filepath.Join(root, dir, name),
				[]byte("package main\n\nimport \"github.com/urfave/cli/v3\"\n\nvar _ = cli.Command{}\n"), 0o600))
		}
	}

	tree, err := Collect(suite.Dir(root))
	require.NoError(t, err)

	programs := tree.Programs()
	require.Len(t, programs, 2, "one program per package, not per file")
	assert.Equal(t, filepath.Join(root, "a"), programs[0].Dir())
	assert.Equal(t, filepath.Join(root, "b"), programs[1].Dir(), "ordered by directory")
	assert.Equal(t, SourcePath(filepath.ToSlash(filepath.Join(root, "a", "main.go"))), programs[0].Path())
}

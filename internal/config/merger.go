package config

import (
	"os"
	"path/filepath"
	"strings"
)

// FileReader reads a file's bytes; injected so config merging is testable without
// a real base config on disk.
type FileReader func(path string) ([]byte, error)

// TempWriter writes data to a fresh temporary file and returns its path plus a
// cleanup that removes it; injected so the effective-config write is testable.
type TempWriter func(data []byte) (path string, cleanup func(), err error)

// Merger is the generic, per-tool config-merge capability: given a tool's
// base config filenames, its config-flag prefix, the per-repo overlays, and the
// repo directory, it produces the tool's --config argument pointing at an effective
// config (base + overlays). It is reused by every config-file tool — golangci-lint
// is just one configured instance — so no tool is special-cased in the merge logic.
//
// Read and Temp are the filesystem seams. Both default to the real filesystem
// when unset, so a caller wiring the production merger supplies neither and only
// a test names them.
type Merger struct {
	Flag      string
	BaseDir   string
	Read      FileReader
	Temp      TempWriter
	BaseNames []string
	Overlays  []Overlay
}

// reader is the configured file reader, or the real filesystem.
func (m Merger) reader() FileReader {
	if m.Read == nil {
		return os.ReadFile
	}
	return m.Read
}

// temp is the configured temp writer, or the real filesystem.
func (m Merger) temp() TempWriter {
	if m.Temp == nil {
		return osTempWriter
	}
	return m.Temp
}

// readBase returns the first existing base config under BaseDir, or empty bytes
// when none is present (the overlays then define the whole config).
func (m Merger) readBase() []byte {
	read := m.reader()
	for _, name := range m.BaseNames {
		if data, err := read(filepath.Join(m.BaseDir, name)); err == nil {
			return data
		}
	}
	return nil
}

// Args builds the tool's config argument: with overlays it merges them onto the
// base and writes the effective config to a temp file, returning the config flag
// and a cleanup; with no overlays it returns no extra args (and a no-op cleanup),
// leaving config discovery to the tool itself — the pre-merge behavior.
func (m Merger) Args() ([]string, func(), error) {
	if len(m.Overlays) == 0 {
		return nil, func() {}, nil
	}
	base, err := parseTree(m.readBase())
	if err != nil {
		return nil, func() {}, err
	}
	data, err := marshalTree(mergeTree(base, m.Overlays))
	if err != nil {
		return nil, func() {}, err
	}
	path, cleanup, err := m.temp()(data)
	if err != nil {
		return nil, func() {}, err
	}
	return []string{strings.ReplaceAll(m.Flag, configPathPlaceholder, path)}, cleanup, nil
}

// configPathPlaceholder is substituted with the effective config file's path in a
// Spec's Flag template (e.g. "--config={path}").
const configPathPlaceholder = "{path}"

// osTempCreate opens a fresh temp file; indirected so the write-failure path is
// testable (a test substitutes a create that returns an already-closed file).
var osTempCreate = func() (*os.File, error) { return os.CreateTemp("", "stickler-config-*.yaml") }

// osTempWriter is the production TempWriter, writing the effective config to a
// uniquely-named temp file and returning a cleanup that removes it.
func osTempWriter(data []byte) (string, func(), error) {
	file, err := osTempCreate()
	if err != nil {
		return "", func() {}, err
	}
	if writeErr := writeAndClose(file, data); writeErr != nil {
		_ = os.Remove(file.Name())
		return "", func() {}, writeErr
	}
	return file.Name(), func() { _ = os.Remove(file.Name()) }, nil
}

// writeAndClose writes data to file and closes it, returning the first error.
func writeAndClose(file *os.File, data []byte) error {
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

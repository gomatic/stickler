package stickler

import (
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// The generic config document: parse to a tree, deep-merge layers, marshal
// back. Kept apart from the merger that decides WHICH layers apply, because
// this half knows nothing about runners or repositories — it is document
// arithmetic, and it is where "an overlay must never mutate the base" lives.

// mutates a caller's map; nested maps are copied as merges recurse into them.
func cloneMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for key, value := range m {
		out[key] = value
	}
	return out
}

// ParseTree decodes a base config document into a generic tree, or an empty tree
// when data is empty or null. A malformed document is a configuration error.
func ParseTree(data []byte) (map[string]any, error) {
	var tree map[string]any
	if err := yaml.Unmarshal(data, &tree); err != nil {
		return nil, ErrConfig.With(err)
	}
	if tree == nil {
		return map[string]any{}, nil
	}
	return tree, nil
}

// MarshalTree renders an effective configuration tree as a YAML document.
func MarshalTree(tree map[string]any) ([]byte, error) {
	data, err := yaml.Marshal(tree)
	if err != nil {
		return nil, ErrConfig.With(err)
	}
	return data, nil
}

// FileReader reads a file's bytes; injected so config merging is testable without
// a real base config on disk.
type FileReader func(path string) ([]byte, error)

// TempWriter writes data to a fresh temporary file and returns its path plus a
// cleanup that removes it; injected so the effective-config write is testable.
type TempWriter func(data []byte) (path string, cleanup func(), err error)

// ConfigMerger is the generic, per-tool config-merge capability: given a tool's
// base config filenames, its config-flag prefix, the per-repo overlays, and the
// repo directory, it produces the tool's --config argument pointing at an effective
// config (base + overlays). It is reused by every config-file tool — golangci-lint
// is just one configured instance — so no tool is special-cased in the merge logic.
type ConfigMerger struct {
	Flag      string
	BaseDir   string
	Read      FileReader
	Temp      TempWriter
	BaseNames []string
	Overlays  []Overlay
}

// readBase returns the first existing base config under BaseDir, or empty bytes
// when none is present (the overlays then define the whole config).
func (m ConfigMerger) readBase() []byte {
	for _, name := range m.BaseNames {
		if data, err := m.Read(filepath.Join(m.BaseDir, name)); err == nil {
			return data
		}
	}
	return nil
}

// Args builds the tool's config argument: with overlays it merges them onto the
// base and writes the effective config to a temp file, returning the config flag
// and a cleanup; with no overlays it returns no extra args (and a no-op cleanup),
// leaving config discovery to the tool itself — the pre-merge behavior.
func (m ConfigMerger) Args() ([]Arg, func(), error) {
	if len(m.Overlays) == 0 {
		return nil, func() {}, nil
	}
	base, err := ParseTree(m.readBase())
	if err != nil {
		return nil, func() {}, err
	}
	data, err := MarshalTree(MergeTree(base, m.Overlays))
	if err != nil {
		return nil, func() {}, err
	}
	path, cleanup, err := m.Temp(data)
	if err != nil {
		return nil, func() {}, err
	}
	return []Arg{Arg(strings.ReplaceAll(m.Flag, configPathPlaceholder, path))}, cleanup, nil
}

// configPathPlaceholder is substituted with the effective config file's path in a
// ConfigSpec's Flag template (e.g. "--config={path}").
const configPathPlaceholder = "{path}"

// osTempCreate opens a fresh temp file; indirected so the write-failure path is
// testable (a test substitutes a create that returns an already-closed file).
var osTempCreate = func() (*os.File, error) { return os.CreateTemp("", "stickler-config-*.yaml") }

// OSTempWriter is the production TempWriter, writing the effective config to a
// uniquely-named temp file and returning a cleanup that removes it.
func OSTempWriter(data []byte) (string, func(), error) {
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

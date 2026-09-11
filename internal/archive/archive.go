package archive

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const maxEntrySize = 64 << 20

type Package struct {
	Files map[string][]byte
}

func Read(path string) (*Package, error) {
	r, err := zip.OpenReader(path)
	if err != nil {
		return nil, fmt.Errorf("open nupkg: %w", err)
	}
	defer r.Close()
	files := make(map[string][]byte, len(r.File))
	for _, f := range r.File {
		name := filepath.ToSlash(filepath.Clean(f.Name))
		if name == "." || strings.HasPrefix(name, "../") || filepath.IsAbs(name) {
			return nil, fmt.Errorf("unsafe archive path %q", f.Name)
		}
		if f.FileInfo().IsDir() {
			continue
		}
		if f.UncompressedSize64 > maxEntrySize {
			return nil, fmt.Errorf("archive entry too large: %s", name)
		}
		rc, err := f.Open()
		if err != nil {
			return nil, fmt.Errorf("open archive entry %s: %w", name, err)
		}
		data, readErr := io.ReadAll(io.LimitReader(rc, maxEntrySize+1))
		closeErr := rc.Close()
		if readErr != nil {
			return nil, fmt.Errorf("read archive entry %s: %w", name, readErr)
		}
		if closeErr != nil {
			return nil, fmt.Errorf("close archive entry %s: %w", name, closeErr)
		}
		if len(data) > maxEntrySize {
			return nil, fmt.Errorf("archive entry too large: %s", name)
		}
		files[name] = data
	}
	return &Package{Files: files}, nil
}

func (p *Package) Write(path string) (err error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create output: %w", err)
	}
	defer func() {
		if closeErr := f.Close(); err == nil && closeErr != nil {
			err = fmt.Errorf("close output: %w", closeErr)
		}
	}()
	zw := zip.NewWriter(f)
	for name, data := range p.Files {
		w, createErr := zw.Create(name)
		if createErr != nil {
			return fmt.Errorf("create archive entry %s: %w", name, createErr)
		}
		if _, writeErr := w.Write(data); writeErr != nil {
			return fmt.Errorf("write archive entry %s: %w", name, writeErr)
		}
	}
	if err := zw.Close(); err != nil {
		return fmt.Errorf("finalize nupkg: %w", err)
	}
	return nil
}

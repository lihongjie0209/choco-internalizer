package nuspec

import (
	"archive/zip"
	"encoding/xml"
	"fmt"
	"io"
	"strings"
)

const maxNuspecSize = 8 << 20

type Package struct {
	Metadata Metadata `xml:"metadata"`
}

type Metadata struct {
	ID           string       `xml:"id"`
	Version      string       `xml:"version"`
	Title        string       `xml:"title"`
	Authors      string       `xml:"authors"`
	Description  string       `xml:"description"`
	Summary      string       `xml:"summary"`
	Tags         string       `xml:"tags"`
	ProjectURL   string       `xml:"projectUrl"`
	LicenseURL   string       `xml:"licenseUrl"`
	Dependencies Dependencies `xml:"dependencies"`
}

type Dependency struct {
	ID      string `xml:"id,attr"`
	Version string `xml:"version,attr"`
}

type DependencyGroup struct {
	Dependencies []Dependency `xml:"dependency"`
}

type Dependencies struct {
	Direct []Dependency      `xml:"dependency"`
	Groups []DependencyGroup `xml:"group"`
}

func (m Metadata) AllDependencies() []Dependency {
	result := append([]Dependency(nil), m.Dependencies.Direct...)
	for _, group := range m.Dependencies.Groups {
		result = append(result, group.Dependencies...)
	}
	return result
}

func Parse(data []byte) (Metadata, error) {
	var pkg Package
	if err := xml.Unmarshal(data, &pkg); err != nil {
		return Metadata{}, fmt.Errorf("parse nuspec: %w", err)
	}
	if pkg.Metadata.ID == "" || pkg.Metadata.Version == "" {
		return Metadata{}, fmt.Errorf("parse nuspec: id and version are required")
	}
	return pkg.Metadata, nil
}

func ReadPackage(path string) (Metadata, error) {
	reader, err := zip.OpenReader(path)
	if err != nil {
		return Metadata{}, fmt.Errorf("open nupkg metadata: %w", err)
	}
	defer reader.Close()
	for _, file := range reader.File {
		if !strings.HasSuffix(strings.ToLower(file.Name), ".nuspec") {
			continue
		}
		if file.UncompressedSize64 > maxNuspecSize {
			return Metadata{}, fmt.Errorf("nuspec exceeds maximum size")
		}
		entry, err := file.Open()
		if err != nil {
			return Metadata{}, fmt.Errorf("open nuspec: %w", err)
		}
		data, readErr := io.ReadAll(io.LimitReader(entry, maxNuspecSize+1))
		closeErr := entry.Close()
		if readErr != nil {
			return Metadata{}, fmt.Errorf("read nuspec: %w", readErr)
		}
		if closeErr != nil {
			return Metadata{}, fmt.Errorf("close nuspec: %w", closeErr)
		}
		if len(data) > maxNuspecSize {
			return Metadata{}, fmt.Errorf("nuspec exceeds maximum size")
		}
		return Parse(data)
	}
	return Metadata{}, fmt.Errorf("nupkg does not contain a nuspec")
}

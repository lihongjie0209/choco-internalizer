package nuspec

import (
	"encoding/xml"
	"fmt"
)

type Package struct {
	Metadata Metadata `xml:"metadata"`
}

type Metadata struct {
	ID           string       `xml:"id"`
	Version      string       `xml:"version"`
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

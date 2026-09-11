package nuspec

import "testing"

func TestParse(t *testing.T) {
	t.Parallel()
	got, err := Parse([]byte("<?xml version=\"1.0\"?><package><metadata><id>demo</id><version>1.2.3</version><dependencies><dependency id=\"direct\" version=\"[1.0]\"/><group><dependency id=\"grouped\" version=\"2.0\"/></group></dependencies></metadata></package>"))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if got.ID != "demo" || got.Version != "1.2.3" {
		t.Fatalf("Parse() = %#v", got)
	}
	dependencies := got.AllDependencies()
	if len(dependencies) != 2 || dependencies[0].ID != "direct" || dependencies[1].ID != "grouped" {
		t.Fatalf("AllDependencies() = %#v", dependencies)
	}
}

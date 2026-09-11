package powershell

import (
	"strings"
	"testing"
)

func TestRewrite(t *testing.T) {
	t.Parallel()
	script := "$packageName = 'demo'\n$url64bit = 'https://downloads.example.test/demo.exe'\nInstall-ChocolateyPackage -PackageName $packageName -FileType exe -Url64bit $url64bit -SilentArgs '/S'"
	got, resources, err := Rewrite(script)
	if err != nil {
		t.Fatalf("Rewrite() error = %v", err)
	}
	if len(resources) != 1 || resources[0].Filename != "demo.exe" {
		t.Fatalf("Rewrite() resources = %#v", resources)
	}
	for _, expected := range []string{"$toolsDir = Split-Path", "$file64 = Join-Path $toolsDir 'demo.exe'", "Install-ChocolateyInstallPackage", "-File64 $file64"} {
		if !strings.Contains(got, expected) {
			t.Errorf("Rewrite() missing %q:\n%s", expected, got)
		}
	}
	if httpReference.MatchString(got) {
		t.Errorf("Rewrite() retained external URL: %s", got)
	}
}

func TestRewriteRejectsUnsupportedHelper(t *testing.T) {
	t.Parallel()
	_, _, err := Rewrite("Get-ChocolateyWebFile -Url 'https://example.test/a.exe'")
	if err == nil {
		t.Fatal("Rewrite() error = nil, want unsupported helper error")
	}
}

func TestRewriteHandlesMultilineCommandAndIgnoresCommentText(t *testing.T) {
	t.Parallel()
	script := "# Install-ChocolateyPackage is mentioned in documentation\n$url = \"https://example.test/app.msi\"\nInstall-ChocolateyPackage `\n  -PackageName demo `\n  -Url $url"
	got, resources, err := Rewrite(script)
	if err != nil {
		t.Fatalf("Rewrite() error = %v", err)
	}
	if len(resources) != 1 {
		t.Fatalf("Rewrite() resources = %#v", resources)
	}
	if !strings.Contains(got, "-File $file") {
		t.Fatalf("Rewrite() did not rewrite multiline parameter:\n%s", got)
	}
	if !strings.Contains(got, "# Install-ChocolateyPackage is mentioned") {
		t.Fatalf("Rewrite() modified comment:\n%s", got)
	}
}

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
	for _, expected := range []string{"$toolsDir = Split-Path", "$url64bit = ([Uri](Join-Path $toolsDir 'demo.exe')).AbsoluteUri", "Install-ChocolateyPackage", "-Url64bit $url64bit"} {
		if !strings.Contains(got, expected) {
			t.Errorf("Rewrite() missing %q:\n%s", expected, got)
		}
	}
	if httpReference.MatchString(got) {
		t.Errorf("Rewrite() retained external URL: %s", got)
	}
}

func TestRewriteSupportsWebFileHelper(t *testing.T) {
	t.Parallel()
	got, resources, err := Rewrite("Get-ChocolateyWebFile -Url 'https://example.test/a.exe'")
	if err != nil {
		t.Fatalf("Rewrite() error = %v", err)
	}
	if len(resources) != 1 || resources[0].Filename != "a.exe" {
		t.Fatalf("Rewrite() resources = %#v", resources)
	}
	if !strings.Contains(got, "([Uri](Join-Path $toolsDir 'a.exe')).AbsoluteUri") {
		t.Fatalf("Rewrite() = %s", got)
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
	if !strings.Contains(got, "-Url $url") {
		t.Fatalf("Rewrite() did not rewrite multiline parameter:\n%s", got)
	}
	if !strings.Contains(got, "# Install-ChocolateyPackage is mentioned") {
		t.Fatalf("Rewrite() modified comment:\n%s", got)
	}
}

func TestRewriteSupportsHashtableAndDynamicVersionURL(t *testing.T) {
	t.Parallel()
	script := `$version = '9.7.1'
$packageArgs = @{
  Url = "https://example.test/tool-$version.zip"
  Url64bit = 'https://example.test/tool-x64.zip'
}
Install-ChocolateyZipPackage @packageArgs`
	got, resources, err := Rewrite(script)
	if err != nil {
		t.Fatalf("Rewrite() error = %v", err)
	}
	if len(resources) != 2 {
		t.Fatalf("Rewrite() resources = %#v", resources)
	}
	if resources[0].URL != "https://example.test/tool-9.7.1.zip" && resources[1].URL != "https://example.test/tool-9.7.1.zip" {
		t.Fatalf("Rewrite() did not resolve version: %#v", resources)
	}
	if httpReference.MatchString(got) {
		t.Fatalf("Rewrite() retained download URL: %s", got)
	}
}

func TestRewriteIgnoresInformationalURLs(t *testing.T) {
	t.Parallel()
	script := `Write-Host 'See https://example.test/help for documentation'`
	got, resources, err := Rewrite(script)
	if err != nil {
		t.Fatalf("Rewrite() error = %v", err)
	}
	if got != script || len(resources) != 0 {
		t.Fatalf("Rewrite() modified informational URL: %s, %#v", got, resources)
	}
}

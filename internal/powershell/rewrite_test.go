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
	for _, expected := range []string{"$toolsDir = Split-Path", "$url64bit = (Join-Path $toolsDir 'demo.exe')", "Install-ChocolateyInstallPackage", "-File64 $url64bit"} {
		if !strings.Contains(got, expected) {
			t.Errorf("Rewrite() missing %q:\n%s", expected, got)
		}
	}
	if httpReference.MatchString(got) {
		t.Errorf("Rewrite() retained external URL: %s", got)
	}
}

func TestRewriteUsesOriginalExecutableNameForLocalInstaller(t *testing.T) {
	t.Parallel()
	script := `$url = 'https://example.test/rustup-init.exe'
$url64 = 'https://example.test/rustup-init-x64.exe'
$packageArgs = @{
  url = $url
  url64bit = $url64
  silentArgs = '-y'
}
Install-ChocolateyPackage @packageArgs`
	got, resources, err := Rewrite(script)
	if err != nil {
		t.Fatalf("Rewrite() error = %v", err)
	}
	if len(resources) != 2 {
		t.Fatalf("Rewrite() resources = %#v", resources)
	}
	for _, expected := range []string{
		"$url = (Join-Path $toolsDir 'rustup-init.exe')",
		"$url64 = (Join-Path $toolsDir 'rustup-init-x64.exe')",
		"file = $url",
		"file64 = $url64",
		"Install-ChocolateyInstallPackage @packageArgs",
	} {
		if !strings.Contains(got, expected) {
			t.Errorf("Rewrite() missing %q:\n%s", expected, got)
		}
	}
	if strings.Contains(strings.ToLower(got), "install-chocolateypackage") {
		t.Fatalf("Rewrite() retained download helper:\n%s", got)
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
	if !strings.Contains(got, "-File $url") {
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

func TestRewriteSupportsFirefoxArchitectureBuildMap(t *testing.T) {
	t.Parallel()
	script := `$locale = 'en-US'
$builds = @{
  'x86'   = @{ Url = "https://download.mozilla.org/?product=firefox-155.0.1-ssl&os=win&lang=${locale}"; Checksum = $checksums.Win32 }
  'x64'   = @{ Url = "https://download.mozilla.org/?product=firefox-155.0.1-ssl&os=win64&lang=${locale}"; Checksum = $checksums.Win64 }
  'arm64' = @{ Url = "https://download.mozilla.org/?product=firefox-155.0.1-ssl&os=win64-aarch64&lang=${locale}"; Checksum = $checksums.Win64Arm64 }
}
$build = Get-MozillaBuild -builds $builds
$packageArgs = @{ Url = $build.Url; Checksum = $build.Checksum }
Install-ChocolateyPackage @packageArgs`

	got, resources, err := Rewrite(script)
	if err != nil {
		t.Fatalf("Rewrite() error = %v", err)
	}
	if len(resources) != 3 {
		t.Fatalf("Rewrite() resources = %#v, want three architecture installers", resources)
	}
	wantFiles := map[string]bool{
		"firefox-155.0.1-ssl-win.exe":           false,
		"firefox-155.0.1-ssl-win64.exe":         false,
		"firefox-155.0.1-ssl-win64-aarch64.exe": false,
	}
	for _, resource := range resources {
		if _, ok := wantFiles[resource.Filename]; !ok {
			t.Errorf("unexpected resource filename %q", resource.Filename)
		} else {
			wantFiles[resource.Filename] = true
		}
	}
	for filename, found := range wantFiles {
		if !found {
			t.Errorf("missing resource %q", filename)
		}
	}
	if strings.Contains(got, "download.mozilla.org") {
		t.Fatalf("Rewrite() retained Mozilla download URL:\n%s", got)
	}
	if !strings.Contains(got, "Install-ChocolateyInstallPackage") {
		t.Fatalf("Rewrite() retained network install helper:\n%s", got)
	}
}

func TestRewriteResolvesPowerShellInterpolationForms(t *testing.T) {
	t.Parallel()
	script := `$version = '1.22.22'
$url = "https://example.test/$($version)/tool-${version}.msi"
$url64 = "https://example.test/$($env:ChocolateyPackageVersion)/tool-x64.msi"
Install-ChocolateyPackage -Url $url -Url64bit $url64`
	got, resources, err := RewriteWithOptions(script, Options{PackageVersion: "1.22.22"})
	if err != nil {
		t.Fatalf("Rewrite() error = %v", err)
	}
	if len(resources) != 2 || resources[0].URL != "https://example.test/1.22.22/tool-1.22.22.msi" || resources[1].URL != "https://example.test/1.22.22/tool-x64.msi" {
		t.Fatalf("Rewrite() resources = %#v", resources)
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

func TestRewritePreservesUTF8BOMAtFileStart(t *testing.T) {
	t.Parallel()
	script := "\uFEFF$ErrorActionPreference = 'Stop'\r\n" +
		"$url = 'https://downloads.example.test/app.exe'\r\n" +
		"Install-ChocolateyPackage -PackageName demo -FileType exe -Url $url"

	got, resources, err := Rewrite(script)
	if err != nil {
		t.Fatalf("Rewrite() error = %v", err)
	}
	if len(resources) != 1 {
		t.Fatalf("Rewrite() resources = %d, want 1", len(resources))
	}
	if !strings.HasPrefix(got, "\uFEFF$toolsDir = ") {
		t.Fatalf("Rewrite() did not preserve BOM at byte zero: %q", got[:min(len(got), 80)])
	}
	if strings.Contains(strings.TrimPrefix(got, "\uFEFF"), "\uFEFF") {
		t.Fatal("Rewrite() left a BOM inside the script")
	}
	if !strings.HasPrefix(got, string([]byte{0xEF, 0xBB, 0xBF})) {
		t.Fatalf("Rewrite() prefix = % X, want UTF-8 BOM", []byte(got)[:3])
	}
}

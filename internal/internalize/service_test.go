package internalize

import (
	"strings"
	"testing"

	"github.com/lihongjie0209/choco-internalizer/internal/powershell"
)

func TestPrepareInstallScriptInternalizesFirefoxLocale(t *testing.T) {
	t.Parallel()
	script := `$locale = 'en-US' # default used by the package updater
$locale = GetLocale -localeFile "$toolsPath\LanguageChecksums.csv"
$builds = @{
  'x86' = @{ Url = "https://download.mozilla.org/?product=firefox-155.0.1-ssl&os=win&lang=${locale}" }
  'x64' = @{ Url = "https://download.mozilla.org/?product=firefox-155.0.1-ssl&os=win64&lang=${locale}" }
}
Install-ChocolateyPackage -Url $builds.x64.Url`

	prepared := prepareInstallScript("Firefox", script)
	if strings.Contains(prepared, "$locale = GetLocale") {
		t.Fatalf("prepareInstallScript() retained runtime locale selection:\n%s", prepared)
	}
	rewritten, resources, err := powershell.Rewrite(prepared)
	if err != nil {
		t.Fatalf("Rewrite() error = %v", err)
	}
	if len(resources) != 2 {
		t.Fatalf("Rewrite() resources = %#v, want two architecture installers", resources)
	}
	if strings.Contains(rewritten, "download.mozilla.org") {
		t.Fatalf("Rewrite() retained Firefox download URL:\n%s", rewritten)
	}
}

func TestPrepareInstallScriptDoesNotChangeOtherPackages(t *testing.T) {
	t.Parallel()
	script := "$locale = GetLocale"
	if got := prepareInstallScript("thunderbird", script); got != script {
		t.Fatalf("prepareInstallScript() = %q, want unchanged script", got)
	}
}

# choco-internalizer

[![Sync Chocolatey packages](https://github.com/lihongjie0209/choco-internalizer/actions/workflows/sync.yml/badge.svg)](https://github.com/lihongjie0209/choco-internalizer/actions/workflows/sync.yml)

Open-source Chocolatey package internalizer. It downloads installer resources,
embeds them in the `.nupkg`, rewrites supported Chocolatey helpers to local-file
helpers, and fails if external URLs remain.

The tool never executes scripts from the source package.

## Current support

- Local `.nupkg` input
- Static `$url` and `$url64bit` assignments
- PowerShell syntax-tree analysis through `tree-sitter-powershell`
- `Install-ChocolateyPackage` to `Install-ChocolateyInstallPackage`
- Download size limits
- ZIP path traversal and oversized-entry protection
- Residual external URL audit
- Pass-through verification for packages that already embed all resources

## Build and run

```bash
make test
make build
./bin/choco-internalizer internalize --input package.nupkg --output dist
```

Validate/internalize and publish in one command (already-internalized packages are
published too):

```bash
CHOCO_API_KEY=... ./bin/choco-internalizer internalize \
  --input package.nupkg \
  --output dist \
  --repository https://choco.lihongjie.cn
```

Upload the generated package:

```powershell
choco push .\dist\package.version.nupkg `
  --source https://choco.lihongjie.cn/api/v2/ `
  --api-key $env:CHOCO_API_KEY
```

Packages using computed URLs or unsupported download helpers fail closed and
must be handled by a future rule plugin or manually reviewed.

Recursively internalize dependencies and publish them before each parent package:

```bash
CHOCO_API_KEY=... ./bin/choco-internalizer sync 7zip git curl \
  --repository https://choco.lihongjie.cn
```

`packages.txt` is synchronized daily by GitHub Actions. A compatibility failure
is emitted as a workflow warning and recorded in the job summary, while the
remaining roots continue. Configure the repository secret `CHOCO_API_KEY`.

The destination repository performs idempotent `id + version` checks and keeps
only the three most recently published versions of every package, including
recursively resolved dependencies.

package internalize

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/lihongjie0209/choco-internalizer/internal/archive"
	"github.com/lihongjie0209/choco-internalizer/internal/nuspec"
	"github.com/lihongjie0209/choco-internalizer/internal/powershell"
)

type HTTPClient interface {
	Do(*http.Request) (*http.Response, error)
}
type Options struct{ MaxDownloadSize int64 }
type Service struct {
	client          HTTPClient
	maxDownloadSize int64
}
type ResourceReport struct {
	URL, Path, SHA256 string
	Size              int64
}
type Report struct {
	Output       string
	ID           string
	Version      string
	Dependencies []nuspec.Dependency
	Resources    []ResourceReport
}

func New(client HTTPClient, options Options) *Service {
	limit := options.MaxDownloadSize
	if limit <= 0 {
		limit = 4 << 30
	}
	return &Service{client: client, maxDownloadSize: limit}
}

func (s *Service) Run(ctx context.Context, input, output string) (Report, error) {
	pkg, err := archive.Read(input)
	if err != nil {
		return Report{}, err
	}
	var metadata nuspec.Metadata
	found := false
	for name, data := range pkg.Files {
		if strings.HasSuffix(strings.ToLower(name), ".nuspec") {
			metadata, err = nuspec.Parse(data)
			found = true
			break
		}
	}
	if err != nil {
		return Report{}, err
	}
	if !found {
		return Report{}, fmt.Errorf("nupkg does not contain a nuspec")
	}
	report := Report{ID: metadata.ID, Version: metadata.Version, Dependencies: metadata.AllDependencies()}
	for name, data := range pkg.Files {
		if !strings.EqualFold(filepath.Base(name), "chocolateyInstall.ps1") {
			continue
		}
		rewritten, resources, rewriteErr := powershell.Rewrite(string(data))
		if rewriteErr != nil {
			return Report{}, fmt.Errorf("rewrite %s: %w", name, rewriteErr)
		}
		pkg.Files[name] = []byte(rewritten)
		for _, resource := range resources {
			body, downloadErr := s.download(ctx, resource.URL)
			if downloadErr != nil {
				return Report{}, fmt.Errorf("download %s: %w", resource.URL, downloadErr)
			}
			target := filepath.ToSlash(filepath.Join(filepath.Dir(name), resource.Filename))
			if _, exists := pkg.Files[target]; exists {
				return Report{}, fmt.Errorf("resource target already exists: %s", target)
			}
			pkg.Files[target] = body
			hash := sha256.Sum256(body)
			report.Resources = append(report.Resources, ResourceReport{URL: resource.URL, Path: target, Size: int64(len(body)), SHA256: hex.EncodeToString(hash[:])})
		}
	}
	if filepath.Ext(output) != ".nupkg" {
		output = filepath.Join(output, fmt.Sprintf("%s.%s.nupkg", metadata.ID, metadata.Version))
	}
	if err := pkg.Write(output); err != nil {
		return Report{}, err
	}
	report.Output = output
	return report, nil
}

func (s *Service) download(ctx context.Context, source string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request resource: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("unexpected HTTP status %s", resp.Status)
	}
	if resp.ContentLength > s.maxDownloadSize {
		return nil, fmt.Errorf("resource exceeds maximum size")
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, s.maxDownloadSize+1))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if int64(len(data)) > s.maxDownloadSize {
		return nil, fmt.Errorf("resource exceeds maximum size")
	}
	return data, nil
}

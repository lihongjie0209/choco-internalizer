package syncer

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/lihongjie0209/choco-internalizer/internal/internalize"
	"github.com/lihongjie0209/choco-internalizer/internal/nuspec"
	"github.com/lihongjie0209/choco-internalizer/internal/publish"
)

var versionToken = regexp.MustCompile(`[0-9]+(?:\.[0-9A-Za-z-]+)+`)

type Event struct {
	ID, Version, Status string
}

type Syncer struct {
	http       *resty.Client
	processor  *internalize.Service
	publisher  *publish.Client
	source     string
	repository string
	apiKey     string
	output     string
	states     map[string]uint8
	events     []Event
}

type Options struct {
	Source, Repository, APIKey, Output string
	MaxDownloadSize                    int64
}

func New(options Options) (*Syncer, error) {
	for name, raw := range map[string]string{"source": options.Source, "repository": options.Repository} {
		parsed, err := url.Parse(raw)
		if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
			return nil, fmt.Errorf("%s must be an absolute HTTPS URL", name)
		}
	}
	client := resty.New().SetTimeout(2 * time.Hour).SetRetryCount(2)
	return &Syncer{
		http: client, processor: internalize.New(client.GetClient(), internalize.Options{MaxDownloadSize: options.MaxDownloadSize}),
		publisher: publish.New(), source: strings.TrimRight(options.Source, "/"), repository: options.Repository,
		apiKey: options.APIKey, output: options.Output, states: make(map[string]uint8),
	}, nil
}

func (s *Syncer) Sync(ctx context.Context, packages []string) ([]Event, error) {
	work, err := os.MkdirTemp("", "choco-internalizer-")
	if err != nil {
		return nil, fmt.Errorf("create temporary directory: %w", err)
	}
	defer os.RemoveAll(work)
	for _, id := range packages {
		if err := s.visit(ctx, work, nuspec.Dependency{ID: id}); err != nil {
			return s.events, err
		}
	}
	return s.events, nil
}

func (s *Syncer) visit(ctx context.Context, work string, dependency nuspec.Dependency) error {
	requestedVersion := selectVersion(dependency.Version)
	stateKey := strings.ToLower(dependency.ID) + "@" + requestedVersion
	if s.states[stateKey] == 2 {
		return nil
	}
	if s.states[stateKey] == 1 {
		return fmt.Errorf("dependency cycle detected at %s", stateKey)
	}
	s.states[stateKey] = 1
	input := filepath.Join(work, safeName(dependency.ID)+"-"+safeName(requestedVersion)+".nupkg")
	downloadURL := fmt.Sprintf("%s/package/%s", s.source, url.PathEscape(dependency.ID))
	if requestedVersion != "" {
		downloadURL += "/" + url.PathEscape(requestedVersion)
	}
	response, err := s.http.R().SetContext(ctx).SetOutput(input).Get(downloadURL)
	if err != nil {
		return fmt.Errorf("download package %s: %w", dependency.ID, err)
	}
	if response.IsError() {
		return fmt.Errorf("download package %s: source returned HTTP %d", dependency.ID, response.StatusCode())
	}
	report, err := s.processor.Run(ctx, input, s.output)
	if err != nil {
		return fmt.Errorf("internalize %s: %w", dependency.ID, err)
	}
	resolvedKey := strings.ToLower(report.ID) + "@" + report.Version
	if resolvedKey != stateKey && s.states[resolvedKey] == 1 {
		return fmt.Errorf("dependency cycle detected at %s", resolvedKey)
	}
	s.states[resolvedKey] = 1
	for _, child := range report.Dependencies {
		if err := s.visit(ctx, work, child); err != nil {
			return err
		}
	}
	skipped, err := s.publisher.Push(ctx, s.repository, s.apiKey, report.Output, report.ID, report.Version)
	if err != nil {
		return fmt.Errorf("publish %s %s: %w", report.ID, report.Version, err)
	}
	status := "published"
	if skipped {
		status = "skipped"
	}
	s.events = append(s.events, Event{ID: report.ID, Version: report.Version, Status: status})
	s.states[stateKey], s.states[resolvedKey] = 2, 2
	return nil
}

func selectVersion(constraint string) string {
	constraint = strings.TrimSpace(constraint)
	if constraint == "" {
		return ""
	}
	if strings.HasPrefix(constraint, "[") && strings.HasSuffix(constraint, "]") && !strings.Contains(constraint, ",") {
		return strings.TrimSpace(constraint[1 : len(constraint)-1])
	}
	return versionToken.FindString(constraint)
}

func safeName(value string) string {
	if value == "" {
		return "latest"
	}
	return strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("._-", r) {
			return r
		}
		return '_'
	}, value)
}

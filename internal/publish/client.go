package publish

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
)

type Client struct{ http *resty.Client }

func New() *Client {
	return &Client{http: resty.New().SetTimeout(2 * time.Hour).SetRetryCount(2)}
}

func (c *Client) Push(ctx context.Context, repository, apiKey, packagePath, id, version string) (bool, error) {
	base, err := url.Parse(repository)
	if err != nil || base.Scheme != "https" || base.Host == "" {
		return false, fmt.Errorf("repository must be an absolute HTTPS URL")
	}
	root := strings.TrimRight(repository, "/")
	checkURL := fmt.Sprintf("%s/api/v2/package/%s/%s", root, url.PathEscape(id), url.PathEscape(version))
	check, err := c.http.R().SetContext(ctx).SetHeader("X-NuGet-ApiKey", apiKey).Head(checkURL)
	if err != nil {
		return false, fmt.Errorf("check package existence: %w", err)
	}
	if check.StatusCode() == http.StatusOK {
		return true, nil
	}
	if check.StatusCode() != http.StatusNotFound {
		return false, fmt.Errorf("check package existence: repository returned HTTP %d", check.StatusCode())
	}
	response, err := c.http.R().SetContext(ctx).
		SetHeader("X-NuGet-ApiKey", apiKey).
		SetFile("package", packagePath).
		Post(root + "/api/v2/package")
	if err != nil {
		return false, fmt.Errorf("upload %s: %w", filepath.Base(packagePath), err)
	}
	if response.IsError() {
		return false, fmt.Errorf("upload %s: repository returned HTTP %d", filepath.Base(packagePath), response.StatusCode())
	}
	return false, nil
}

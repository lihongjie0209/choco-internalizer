package publish

import (
	"context"
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
)

type Client struct{ http *resty.Client }

type Package struct {
	ID           string `json:"id"`
	Version      string `json:"version"`
	Title        string `json:"title,omitempty"`
	Authors      string `json:"authors,omitempty"`
	Description  string `json:"description,omitempty"`
	Summary      string `json:"summary,omitempty"`
	Tags         string `json:"tags,omitempty"`
	ProjectURL   string `json:"projectUrl,omitempty"`
	LicenseURL   string `json:"licenseUrl,omitempty"`
	Dependencies string `json:"dependencies,omitempty"`
	PackageSize  int64  `json:"packageSize"`
	PackageHash  string `json:"packageHash"`
}

type multipartStart struct {
	Exists   bool   `json:"exists"`
	Key      string `json:"key"`
	UploadID string `json:"uploadId"`
}

type uploadedPart struct {
	PartNumber int    `json:"partNumber"`
	ETag       string `json:"etag"`
}

const (
	directUploadLimit = 80 << 20
	partSize          = 50 << 20
)

func New() *Client {
	client := resty.New().SetTimeout(2 * time.Hour).SetRetryCount(3).
		SetRetryWaitTime(time.Second).SetRetryMaxWaitTime(15 * time.Second)
	client.AddRetryCondition(func(response *resty.Response, err error) bool {
		return err != nil || response.StatusCode() == http.StatusTooManyRequests || response.StatusCode() >= 500
	})
	return &Client{http: client}
}

func (c *Client) Push(ctx context.Context, repository, apiKey, packagePath string, pkg Package) (bool, error) {
	base, err := url.Parse(repository)
	if err != nil || base.Scheme != "https" || base.Host == "" {
		return false, fmt.Errorf("repository must be an absolute HTTPS URL")
	}
	root := strings.TrimRight(repository, "/")
	checkURL := fmt.Sprintf("%s/api/v2/package/%s/%s", root, url.PathEscape(pkg.ID), url.PathEscape(pkg.Version))
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
	info, err := os.Stat(packagePath)
	if err != nil {
		return false, fmt.Errorf("stat package: %w", err)
	}
	if info.Size() > directUploadLimit {
		return c.pushMultipart(ctx, root, apiKey, packagePath, pkg)
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

func (c *Client) pushMultipart(ctx context.Context, root, apiKey, packagePath string, pkg Package) (bool, error) {
	file, err := os.Open(packagePath)
	if err != nil {
		return false, fmt.Errorf("open package: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return false, fmt.Errorf("stat package: %w", err)
	}
	hash := sha512.New()
	if _, err := io.Copy(hash, file); err != nil {
		return false, fmt.Errorf("hash package: %w", err)
	}
	pkg.PackageSize = info.Size()
	pkg.PackageHash = base64.StdEncoding.EncodeToString(hash.Sum(nil))
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return false, fmt.Errorf("rewind package: %w", err)
	}
	var start multipartStart
	response, err := c.http.R().SetContext(ctx).SetHeader("X-NuGet-ApiKey", apiKey).
		SetBody(map[string]string{"id": pkg.ID, "version": pkg.Version}).SetResult(&start).
		Post(root + "/api/v2/upload/start")
	if err != nil {
		return false, fmt.Errorf("start multipart upload: %w", err)
	}
	if response.IsError() {
		return false, fmt.Errorf("start multipart upload: repository returned HTTP %d", response.StatusCode())
	}
	if start.Exists {
		return true, nil
	}
	if start.Key == "" || start.UploadID == "" {
		return false, fmt.Errorf("start multipart upload: invalid repository response")
	}
	parts := make([]uploadedPart, 0, (info.Size()+partSize-1)/partSize)
	for partNumber, offset := 1, int64(0); offset < info.Size(); partNumber, offset = partNumber+1, offset+partSize {
		size := min(int64(partSize), info.Size()-offset)
		endpoint := root + "/api/v2/upload/part?key=" + url.QueryEscape(start.Key) +
			"&uploadId=" + url.QueryEscape(start.UploadID) + "&partNumber=" + fmt.Sprint(partNumber)
		request, requestErr := http.NewRequestWithContext(ctx, http.MethodPut, endpoint, io.NewSectionReader(file, offset, size))
		if requestErr != nil {
			return false, fmt.Errorf("create upload part %d: %w", partNumber, requestErr)
		}
		request.Header.Set("X-NuGet-ApiKey", apiKey)
		request.ContentLength = size
		partResponse, requestErr := c.http.GetClient().Do(request)
		if requestErr != nil {
			return false, fmt.Errorf("upload part %d: %w", partNumber, requestErr)
		}
		var part uploadedPart
		decodeErr := json.NewDecoder(partResponse.Body).Decode(&part)
		closeErr := partResponse.Body.Close()
		if partResponse.StatusCode < 200 || partResponse.StatusCode >= 300 {
			return false, fmt.Errorf("upload part %d: repository returned HTTP %d", partNumber, partResponse.StatusCode)
		}
		if decodeErr != nil {
			return false, fmt.Errorf("decode upload part %d: %w", partNumber, decodeErr)
		}
		if closeErr != nil {
			return false, fmt.Errorf("close upload part %d response: %w", partNumber, closeErr)
		}
		parts = append(parts, part)
	}
	response, err = c.http.R().SetContext(ctx).SetHeader("X-NuGet-ApiKey", apiKey).
		SetBody(map[string]any{"key": start.Key, "uploadId": start.UploadID, "parts": parts, "package": pkg}).
		Post(root + "/api/v2/upload/complete")
	if err != nil {
		return false, fmt.Errorf("complete multipart upload: %w", err)
	}
	if response.IsError() {
		return false, fmt.Errorf("complete multipart upload: repository returned HTTP %d", response.StatusCode())
	}
	return false, nil
}

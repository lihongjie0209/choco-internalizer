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
	exists, err := c.Exists(ctx, repository, apiKey, pkg.ID, pkg.Version)
	if err != nil {
		return false, err
	}
	if exists {
		return true, nil
	}
	root := strings.TrimRight(repository, "/")
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

func (c *Client) Exists(ctx context.Context, repository, apiKey, id, version string) (bool, error) {
	base, err := url.Parse(repository)
	if err != nil || base.Scheme != "https" || base.Host == "" {
		return false, fmt.Errorf("repository must be an absolute HTTPS URL")
	}
	root := strings.TrimRight(repository, "/")
	checkURL := fmt.Sprintf("%s/api/v2/package/%s/%s", root, url.PathEscape(id), url.PathEscape(version))
	response, err := c.http.R().SetContext(ctx).SetHeader("X-NuGet-ApiKey", apiKey).Head(checkURL)
	if err != nil {
		return false, fmt.Errorf("check package existence: %w", err)
	}
	if response.StatusCode() == http.StatusOK {
		return true, nil
	}
	if response.StatusCode() == http.StatusNotFound {
		return false, nil
	}
	return false, fmt.Errorf("check package existence: repository returned HTTP %d", response.StatusCode())
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
		part, uploadErr := c.uploadPart(ctx, endpoint, apiKey, file, offset, size, partNumber)
		if uploadErr != nil {
			return false, uploadErr
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

func (c *Client) uploadPart(ctx context.Context, endpoint, apiKey string, file *os.File, offset, size int64, partNumber int) (uploadedPart, error) {
	var lastErr error
	for attempt := 0; attempt < 4; attempt++ {
		request, err := http.NewRequestWithContext(ctx, http.MethodPut, endpoint, io.NewSectionReader(file, offset, size))
		if err != nil {
			return uploadedPart{}, fmt.Errorf("create upload part %d: %w", partNumber, err)
		}
		request.Header.Set("X-NuGet-ApiKey", apiKey)
		request.ContentLength = size
		request.GetBody = func() (io.ReadCloser, error) {
			return io.NopCloser(io.NewSectionReader(file, offset, size)), nil
		}
		response, err := c.http.GetClient().Do(request)
		if err == nil && response.StatusCode >= 200 && response.StatusCode < 300 {
			var part uploadedPart
			decodeErr := json.NewDecoder(response.Body).Decode(&part)
			closeErr := response.Body.Close()
			if decodeErr != nil {
				return uploadedPart{}, fmt.Errorf("decode upload part %d: %w", partNumber, decodeErr)
			}
			if closeErr != nil {
				return uploadedPart{}, fmt.Errorf("close upload part %d response: %w", partNumber, closeErr)
			}
			return part, nil
		}
		if response != nil {
			_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
			_ = response.Body.Close()
			if response.StatusCode < 500 && response.StatusCode != http.StatusTooManyRequests {
				return uploadedPart{}, fmt.Errorf("upload part %d: repository returned HTTP %d", partNumber, response.StatusCode)
			}
			lastErr = fmt.Errorf("repository returned HTTP %d", response.StatusCode)
		} else {
			lastErr = err
		}
		if attempt < 3 {
			timer := time.NewTimer(time.Duration(1<<attempt) * time.Second)
			select {
			case <-ctx.Done():
				timer.Stop()
				return uploadedPart{}, fmt.Errorf("upload part %d: %w", partNumber, ctx.Err())
			case <-timer.C:
			}
		}
	}
	return uploadedPart{}, fmt.Errorf("upload part %d after retries: %w", partNumber, lastErr)
}

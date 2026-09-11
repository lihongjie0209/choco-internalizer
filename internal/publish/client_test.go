package publish

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
)

func TestUploadPartRetriesTransientFailure(t *testing.T) {
	t.Parallel()
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) < 2 {
			http.Error(w, "try again", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("content-type", "application/json")
		fmt.Fprint(w, `{"partNumber":1,"etag":"etag-1"}`)
	}))
	defer server.Close()

	file, err := os.CreateTemp(t.TempDir(), "part-*.bin")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err := file.WriteString("package bytes"); err != nil {
		t.Fatal(err)
	}

	part, err := New().uploadPart(t.Context(), server.URL, "secret", file, 0, 13, 1)
	if err != nil {
		t.Fatalf("uploadPart() error = %v", err)
	}
	if part.PartNumber != 1 || part.ETag != "etag-1" {
		t.Fatalf("uploadPart() = %#v", part)
	}
	if attempts.Load() != 2 {
		t.Fatalf("attempts = %d, want 2", attempts.Load())
	}
}

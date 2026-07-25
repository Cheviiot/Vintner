package download

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestHTTPDownloadFileFullDownload(t *testing.T) {
	const body = "the quick brown fox jumps over the lazy dog"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, body)
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "out")
	if err := httpDownloadFile(srv.URL, dest); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != body {
		t.Errorf("downloaded content = %q, want %q", got, body)
	}
}

// rangeServer serves a fixed body and honors byte-range requests, exactly
// like a real payload host (GitHub Releases, nuget.org, etc.) would.
func rangeServer(body string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rng := r.Header.Get("Range")
		if rng == "" {
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, body)
			return
		}
		var start int
		if _, err := fmt.Sscanf(rng, "bytes=%d-", &start); err != nil || start < 0 || start > len(body) {
			w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
			return
		}
		w.Header().Set("Content-Range", "bytes "+strconv.Itoa(start)+"-"+strconv.Itoa(len(body)-1)+"/"+strconv.Itoa(len(body)))
		w.WriteHeader(http.StatusPartialContent)
		fmt.Fprint(w, body[start:])
	}))
}

func TestHTTPDownloadFileResumesFromExistingPart(t *testing.T) {
	const body = "the quick brown fox jumps over the lazy dog"
	srv := rangeServer(body)
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "out")
	partial := body[:10]
	if err := os.WriteFile(dest+".part", []byte(partial), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := httpDownloadFile(srv.URL, dest); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != body {
		t.Errorf("resumed download content = %q, want %q (partial %q should have been continued, not duplicated or lost)", got, body, partial)
	}
}

func TestHTTPDownloadFileRestartsWhenServerIgnoresRange(t *testing.T) {
	const body = "the quick brown fox jumps over the lazy dog"
	// Always answers 200 with the full body, regardless of Range - some
	// servers/CDNs genuinely don't support partial content.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, body)
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "out")
	// A stale/bogus .part that must NOT end up prepended to the real
	// content - if httpDownloadFile appended instead of truncating here,
	// the result would start with this garbage.
	if err := os.WriteFile(dest+".part", []byte("GARBAGE-FROM-A-STALE-ATTEMPT"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := httpDownloadFile(srv.URL, dest); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != body {
		t.Errorf("content = %q, want exactly %q (no leftover garbage prepended)", got, body)
	}
}

func TestHTTPDownloadFileKeepsPartOnMidTransferFailure(t *testing.T) {
	const fullBody = "0123456789"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, fullBody[:5])
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		// Simulate a dropped connection partway through by closing the
		// underlying connection abruptly instead of finishing the body.
		hj, ok := w.(http.Hijacker)
		if !ok {
			return
		}
		conn, _, err := hj.Hijack()
		if err == nil {
			conn.Close()
		}
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "out")
	err := httpDownloadFile(srv.URL, dest)
	if err == nil {
		t.Fatal("expected an error from the truncated connection")
	}

	partial, err := os.ReadFile(dest + ".part")
	if err != nil {
		t.Fatalf("expected the .part file with the bytes received so far to survive a failed download: %v", err)
	}
	if !strings.HasPrefix(fullBody, string(partial)) || len(partial) == 0 {
		t.Errorf(".part content = %q, want a non-empty prefix of %q", partial, fullBody)
	}
}

func TestHTTPDownloadFileRangeNotSatisfiableDiscardsPart(t *testing.T) {
	const body = "short"
	srv := rangeServer(body)
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "out")
	// .part is already longer than the real file - triggers 416 from
	// rangeServer's own bounds check.
	if err := os.WriteFile(dest+".part", []byte("this partial file is way too long"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := httpDownloadFile(srv.URL, dest); err == nil {
		t.Fatal("expected an error on the first (416) attempt")
	}
	if _, err := os.Stat(dest + ".part"); !os.IsNotExist(err) {
		t.Error("expected the stale .part to be discarded after a 416 response")
	}

	// The retry (a fresh caller, no Range header since .part is gone) should
	// now succeed cleanly.
	if err := httpDownloadFile(srv.URL, dest); err != nil {
		t.Fatalf("retry after discarding the stale .part failed: %v", err)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != body {
		t.Errorf("content = %q, want %q", got, body)
	}
}

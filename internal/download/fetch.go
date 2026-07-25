package download

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

const maxConcurrentDownloads = 5
const maxDownloadAttempts = 5

// FetchPayloads fetches every payload of every selected package into
// cacheDir/<packageKey>/<payloadName>, verifying sha256 and skipping files
// already present and correct. allowHashMismatch (used for --only-download)
// warns instead of failing on a hash mismatch.
func FetchPayloads(selected []*Package, cacheDir string, allowHashMismatch bool) error {
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return err
	}

	type task struct {
		payload Payload
		dest    string
		fileID  string
	}
	var tasks []task
	for _, p := range selected {
		if len(p.Payloads) == 0 {
			continue
		}
		dir := filepath.Join(cacheDir, p.Key())
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		for _, pl := range p.Payloads {
			name := pl.Name()
			tasks = append(tasks, task{
				payload: pl,
				dest:    filepath.Join(dir, name),
				fileID:  filepath.Join(p.Key(), name),
			})
		}
	}

	sem := make(chan struct{}, maxConcurrentDownloads)
	var wg sync.WaitGroup
	var totalDownloaded int64
	errCh := make(chan error, len(tasks))

	for _, t := range tasks {
		t := t
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			n, err := fetchOnePayloadWithRetries(t.payload, t.dest, t.fileID, allowHashMismatch)
			if err != nil {
				errCh <- err
				return
			}
			atomic.AddInt64(&totalDownloaded, n)
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			return err
		}
	}

	fmt.Printf("Downloaded %s in total\n", HumanizeBytes(totalDownloaded))
	return nil
}

func fetchOnePayloadWithRetries(payload Payload, dest, fileID string, allowHashMismatch bool) (int64, error) {
	var lastErr error
	for attempt := 0; attempt < maxDownloadAttempts; attempt++ {
		if attempt > 0 {
			time.Sleep(retryBackoff(attempt))
		}
		n, err := tryDownloadPayload(payload, dest, fileID, allowHashMismatch)
		if err == nil {
			return n, nil
		}
		lastErr = err
		fmt.Printf("%v\n", err)
	}
	return 0, fmt.Errorf("giving up on %s after %d attempts: %w", fileID, maxDownloadAttempts, lastErr)
}

// retryBackoff gives a transient failure (network blip, momentary rate
// limiting) a little room to clear before hammering the same URL again:
// 1s, 2s, 4s, 8s, capped at 10s.
func retryBackoff(attempt int) time.Duration {
	d := time.Second << uint(attempt-1)
	if d > 10*time.Second {
		d = 10 * time.Second
	}
	return d
}

func tryDownloadPayload(payload Payload, dest, fileID string, allowHashMismatch bool) (int64, error) {
	if fi, err := os.Stat(dest); err == nil && fi.Mode().IsRegular() {
		if payload.SHA256 != "" {
			sum, err := sha256File(dest)
			if err != nil {
				return 0, err
			}
			if !equalFoldHex(sum, payload.SHA256) {
				fmt.Printf("Incorrect existing file %s, removing\n", fileID)
				os.Remove(dest)
			} else {
				fmt.Printf("Using existing file %s\n", fileID)
				return 0, nil
			}
		} else {
			return 0, nil
		}
	}

	fmt.Printf("Downloading %s (%s)\n", fileID, HumanizeBytes(payload.Size))
	if err := httpDownloadFile(payload.URL, dest); err != nil {
		return 0, err
	}
	if payload.SHA256 != "" {
		sum, err := sha256File(dest)
		if err != nil {
			return 0, err
		}
		if !equalFoldHex(sum, payload.SHA256) {
			if allowHashMismatch {
				fmt.Printf("WARNING: incorrect hash for downloaded file %s\n", fileID)
			} else {
				return 0, fmt.Errorf("incorrect hash for downloaded file %s, aborting", fileID)
			}
		}
	}
	return payload.Size, nil
}

var downloadHTTPClient = &http.Client{Timeout: 30 * time.Minute}

// httpDownloadFile downloads url to dest via a dest+".part" temp file,
// resuming from wherever a previous attempt left off if one exists - MSVC/
// WinSDK/WDK/DXSDK payloads run into the hundreds of MB to multiple GB, so
// restarting an interrupted download from byte 0 (a dropped connection, a
// retry after this same function returned an error) wastes real time and
// bandwidth on a flaky connection. Requests a byte Range starting at the
// existing .part file's size, if any; a server that doesn't honor Range
// (responds 200 instead of 206) gets treated as sending the whole file
// again from byte 0, so the .part is truncated and started over rather
// than getting byte-0 content appended onto existing bytes.
func httpDownloadFile(url, dest string) error {
	tmp := dest + ".part"
	var offset int64
	if fi, err := os.Stat(tmp); err == nil {
		offset = fi.Size()
	}

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	if offset > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
	}
	resp, err := downloadHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var out *os.File
	switch resp.StatusCode {
	case http.StatusOK:
		// No partial-content support (or nothing to resume from): the body
		// is the whole file from byte 0.
		out, err = os.Create(tmp)
	case http.StatusPartialContent:
		out, err = os.OpenFile(tmp, os.O_WRONLY|os.O_APPEND, 0o644)
	case http.StatusRequestedRangeNotSatisfiable:
		// Our .part is already >= the real file size - stale or corrupt.
		// Discard it; the next retry starts clean with no Range header.
		os.Remove(tmp)
		return fmt.Errorf("GET %s: range not satisfiable, discarding partial download and retrying from scratch", url)
	default:
		return fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	if err != nil {
		return err
	}

	if _, err := io.Copy(out, resp.Body); err != nil {
		out.Close()
		// Deliberately not removing tmp here: whatever bytes made it to
		// disk are exactly what the next attempt should resume from.
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, dest)
}

func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func equalFoldHex(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		ca, cb := a[i], b[i]
		if ca >= 'A' && ca <= 'Z' {
			ca += 'a' - 'A'
		}
		if cb >= 'A' && cb <= 'Z' {
			cb += 'a' - 'A'
		}
		if ca != cb {
			return false
		}
	}
	return true
}

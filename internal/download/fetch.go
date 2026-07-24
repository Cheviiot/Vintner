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

func httpDownloadFile(url, dest string) error {
	resp, err := downloadHTTPClient.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	tmp := dest + ".part"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, resp.Body); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
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

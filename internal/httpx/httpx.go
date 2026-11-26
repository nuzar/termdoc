// Package httpx provides a small HTTP client with retries, proxy support and
// human friendly download progress.
package httpx

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"golang.org/x/term"
)

// Client wraps an http.Client configured for docset downloads.
type Client struct {
	hc *http.Client
	UA string
	// Quiet disables progress output.
	Quiet bool
}

// New creates a client. proxy may be empty, or an http:// / https:// / socks5:// URL.
func New(proxy string) (*Client, error) {
	tr := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          10,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   20 * time.Second,
		ResponseHeaderTimeout: 60 * time.Second,
	}
	if proxy != "" {
		u, err := url.Parse(proxy)
		if err != nil {
			return nil, fmt.Errorf("invalid proxy %q: %w", proxy, err)
		}
		switch strings.ToLower(u.Scheme) {
		case "http", "https", "socks5", "socks5h":
		default:
			return nil, fmt.Errorf("unsupported proxy scheme %q (use http:// or socks5://)", u.Scheme)
		}
		tr.Proxy = http.ProxyURL(u)
	}
	return &Client{
		hc: &http.Client{Transport: tr},
		UA: "termdoc/1.0 (+https://github.com/nuzar/termdoc)",
	}, nil
}

// Get fetches url and returns the response body.
func (c *Client) Get(ctx context.Context, rawurl string) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			if err := sleep(ctx, time.Duration(attempt)*time.Second); err != nil {
				return nil, err
			}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawurl, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", c.UA)
		resp, err := c.hc.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			lastErr = err
			continue
		}
		if resp.StatusCode == http.StatusOK {
			return body, nil
		}
		lastErr = fmt.Errorf("%s: HTTP %s", rawurl, resp.Status)
		if !retryable(resp.StatusCode) {
			return nil, lastErr
		}
	}
	return nil, lastErr
}

// Download streams url into w, showing progress on stderr when it is a terminal.
func (c *Client) Download(ctx context.Context, rawurl string, w io.Writer) (int64, error) {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			if err := sleep(ctx, time.Duration(attempt)*time.Second); err != nil {
				return 0, err
			}
		}
		if f, ok := w.(*os.File); ok {
			_ = f.Truncate(0)
			_, _ = f.Seek(0, io.SeekStart)
		}
		n, err := c.downloadOnce(ctx, rawurl, w)
		if err == nil {
			return n, nil
		}
		if IsPermanent(err) {
			return 0, err
		}
		lastErr = err
	}
	return 0, lastErr
}

func (c *Client) downloadOnce(ctx context.Context, rawurl string, w io.Writer) (int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawurl, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("User-Agent", c.UA)
	resp, err := c.hc.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		err := fmt.Errorf("%s: HTTP %s", rawurl, resp.Status)
		if !retryable(resp.StatusCode) {
			return 0, &permanentError{err}
		}
		return 0, err
	}
	pw := &progressWriter{
		label: shorten(rawurl),
		total: resp.ContentLength,
		out:   os.Stderr,
		tty:   !c.Quiet && term.IsTerminal(int(os.Stderr.Fd())),
	}
	n, err := io.Copy(io.MultiWriter(w, pw), resp.Body)
	pw.done()
	if err != nil {
		return n, err
	}
	return n, nil
}

type permanentError struct{ error }

func (e *permanentError) Unwrap() error { return e.error }

func retryable(code int) bool {
	return code == http.StatusTooManyRequests || code == http.StatusRequestTimeout || code >= 500
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// IsPermanent reports whether err should not be retried.
func IsPermanent(err error) bool {
	var p *permanentError
	return errors.As(err, &p)
}

// Logf writes an informational line to stderr unless quiet mode is enabled.
func (c *Client) Logf(format string, args ...any) {
	if c == nil || c.Quiet {
		return
	}
	fmt.Fprintf(os.Stderr, format, args...)
}

type progressWriter struct {
	label   string
	total   int64
	written int64
	out     io.Writer
	tty     bool
	last    time.Time
}

func (p *progressWriter) Write(b []byte) (int, error) {
	p.written += int64(len(b))
	if p.tty && time.Since(p.last) > 120*time.Millisecond {
		p.last = time.Now()
		p.print(false)
	}
	return len(b), nil
}

func (p *progressWriter) done() {
	if p.tty {
		p.print(true)
		fmt.Fprint(p.out, "\r\033[K")
	}
}

func (p *progressWriter) print(final bool) {
	line := fmt.Sprintf("  %s  %s", p.label, humanBytes(p.written))
	if p.total > 0 {
		pct := float64(p.written) / float64(p.total) * 100
		line += fmt.Sprintf(" / %s (%.0f%%)", humanBytes(p.total), pct)
	}
	if final {
		line += "  done"
	}
	fmt.Fprintf(p.out, "\r\033[K%s", line)
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

func shorten(u string) string {
	if i := strings.LastIndex(u, "://"); i >= 0 {
		u = u[i+3:]
	}
	if len(u) > 60 {
		u = u[:57] + "..."
	}
	return u
}

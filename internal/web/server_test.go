package web

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestValidateLoopbackAddress(t *testing.T) {
	for address, want := range map[string]string{
		"127.0.0.1:7420": "127.0.0.1:7420",
		"127.1.2.3:0":    "127.1.2.3:0",
		"[::1]:80":       "[::1]:80",
	} {
		if got, err := ValidateLoopbackAddress(address); err != nil || got != want {
			t.Errorf("%s: got %q, %v", address, got, err)
		}
	}
	for _, address := range []string{"", "0.0.0.0:7420", "192.168.1.2:7420", "localhost:7420", "[::]:7420", "127.0.0.1", "127.0.0.1:99999"} {
		if _, err := ValidateLoopbackAddress(address); err == nil {
			t.Errorf("%q accepted", address)
		}
	}
}

func TestHardenHostAndOrigin(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	h := harden(ok, "127.0.0.1:7420", slog.New(slog.NewTextHandler(io.Discard, nil)), 16)
	cases := []struct {
		host, origin, site string
		status             int
	}{
		{"127.0.0.1:7420", "", "", http.StatusNoContent},
		{"localhost:7420", "", "", http.StatusNoContent},
		{"127.0.0.1:7420", "http://127.0.0.1:7420", "", http.StatusNoContent},
		{"localhost:7420", "http://localhost:7420", "", http.StatusNoContent},
		{"127.0.0.1:7420", "null", "same-origin", http.StatusNoContent},
		{"127.0.0.1:7420", "null", "cross-site", http.StatusForbidden},
		{"127.0.0.1:7420", "http://localhost:7420", "", http.StatusForbidden},
		{"127.0.0.1:7420", "http://evil.example", "", http.StatusForbidden},
		{"evil.example:7420", "", "", http.StatusMisdirectedRequest},
		{"127.0.0.1:9999", "", "", http.StatusMisdirectedRequest},
		{"127.0.0.1", "", "", http.StatusBadRequest},
	}
	for _, tc := range cases {
		request := httptest.NewRequest(http.MethodGet, "/", nil)
		request.Host = tc.host
		if tc.origin != "" {
			request.Header.Set("Origin", tc.origin)
		}
		if tc.site != "" {
			request.Header.Set("Sec-Fetch-Site", tc.site)
		}
		recorder := httptest.NewRecorder()
		h.ServeHTTP(recorder, request)
		if recorder.Code != tc.status {
			t.Errorf("host %s origin %q site %q: status %d, want %d", tc.host, tc.origin, tc.site, recorder.Code, tc.status)
		}
	}

	read := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.ReadAll(r.Body); err != nil {
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	limited := harden(read, "127.0.0.1:7420", slog.New(slog.NewTextHandler(io.Discard, nil)), 16)
	request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(strings.Repeat("x", 17)))
	request.Host = "127.0.0.1:7420"
	recorder := httptest.NewRecorder()
	limited.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("oversized body: status %d", recorder.Code)
	}

	panics := harden(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("boom") }), "127.0.0.1:7420",
		slog.New(slog.NewTextHandler(io.Discard, nil)), 16)
	request = httptest.NewRequest(http.MethodGet, "/", nil)
	request.Host = "127.0.0.1:7420"
	recorder = httptest.NewRecorder()
	panics.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusInternalServerError || strings.Contains(recorder.Body.String(), "boom") {
		t.Errorf("panic: status %d body %q", recorder.Code, recorder.Body.String())
	}
}

func TestServeLifecycle(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	addresses := make(chan string, 1)
	done := make(chan error, 1)
	go func() {
		done <- Serve(ctx, ServerOptions{
			Address:  "127.0.0.1:0",
			Handler:  http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { io.WriteString(w, "ok") }),
			OnListen: func(l net.Listener) { addresses <- l.Addr().String() },
		})
	}()
	address := <-addresses
	response, err := http.Get("http://" + address + "/")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != http.StatusOK || string(body) != "ok" {
		t.Fatalf("status %d body %q", response.StatusCode, body)
	}
	// A DNS-rebinding style request names another host.
	request, _ := http.NewRequest(http.MethodGet, "http://"+address+"/", nil)
	request.Host = "rebind.example:" + strings.Split(address, ":")[1]
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusMisdirectedRequest {
		t.Fatalf("rebinding status %d", response.StatusCode)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serve returned %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("serve did not stop")
	}
	if err := Serve(context.Background(), ServerOptions{Address: "0.0.0.0:0"}); err == nil {
		t.Fatal("non-loopback address accepted")
	}
}

// sourceFiles returns this package's non-test Go files and its assets.
func sourceFiles(t *testing.T) map[string]string {
	t.Helper()
	files := map[string]string{}
	paths, _ := filepath.Glob("*.go")
	assets, _ := filepath.Glob("assets/*")
	for _, path := range append(paths, assets...) {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		files[path] = string(data)
	}
	if len(files) < 7 {
		t.Fatalf("found only %d source files", len(files))
	}
	return files
}

// All user-visible text lives in strings.go, so a locale is one value there.
func TestNoUserTextOutsideUIStrings(t *testing.T) {
	cjk := regexp.MustCompile(`[\x{4e00}-\x{9fff}\x{3000}-\x{303f}\x{ff00}-\x{ffef}]`)
	for path, content := range sourceFiles(t) {
		if path == "strings.go" {
			continue
		}
		for i, line := range strings.Split(content, "\n") {
			if cjk.MatchString(line) {
				t.Errorf("%s:%d has CJK text outside UIStrings: %s", path, i+1, strings.TrimSpace(line))
			}
		}
	}
}

// The Web Board reads recorded state only. Guard against the POC's workflow
// projection vocabulary coming back through a port.
func TestNoWorkflowProjectionVocabulary(t *testing.T) {
	forbidden := regexp.MustCompile(`(?i)attempt|lease|review|claim|executionstarted|projection|effective_?status|reservation|rhizome_?status`)
	comment := regexp.MustCompile(`^\s*(//|/\*|\*)`)
	for path, content := range sourceFiles(t) {
		for i, line := range strings.Split(content, "\n") {
			if comment.MatchString(line) {
				continue
			}
			if match := forbidden.FindString(line); match != "" {
				t.Errorf("%s:%d mentions %q: %s", path, i+1, match, strings.TrimSpace(line))
			}
		}
	}
}

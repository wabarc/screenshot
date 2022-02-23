package screenshot

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os/exec"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/wabarc/helper"
)

func writeHTML(content string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		io.WriteString(w, strings.TrimSpace(content)) // nolint:errcheck
	})
}

func newServer() *httptest.Server {
	return httptest.NewServer(writeHTML(`
<html>
<head>
    <title>Example Domain</title>
</head>

<body>
<div>
    <h1>Example Domain</h1>
    <p>This domain is for use in illustrative examples in documents. You may use this
    domain in literature without prior coordination or asking for permission.</p>
    <p><a href="https://www.iana.org/domains/example">More information...</a></p>
</div>
</body>
</html>
`))
}

func TestScreenshot(t *testing.T) {
	binPath := helper.FindChromeExecPath()
	if _, err := exec.LookPath(binPath); err != nil {
		t.Skip("Chrome headless browser no found, skipped")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	ts := newServer()
	defer ts.Close()

	input, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatal(err)
	}
	shot, err := Screenshot(ctx, input, ScaleFactor(1))
	if err != nil {
		if err == context.DeadlineExceeded {
			t.Error(err.Error(), http.StatusRequestTimeout)
			return
		}
		t.Error(err.Error(), http.StatusServiceUnavailable)
		return
	}

	if reflect.TypeOf(shot) != reflect.TypeOf(&Screenshots{}) {
		t.Fatalf("Unexpected type of Screenshots")
	}

	wantTitle := "Example Domain"
	if shot.Title != wantTitle {
		t.Fatalf("Unexpected title of webpage, got %s instead of %s", shot.Title, wantTitle)
	}

	if shot.Image == nil {
		t.Fail()
	}
}

func TestScreenshotWithRemote(t *testing.T) {
	binPath := helper.FindChromeExecPath()
	if _, err := exec.LookPath(binPath); err != nil {
		t.Skip("Chrome headless browser no found, skipped")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	ts := newServer()
	defer ts.Close()

	cmd := exec.Command(binPath, "--headless", "--disable-gpu", "--no-sandbox", "--remote-debugging-port=9222")
	if err := cmd.Start(); err != nil {
		t.Fatalf("Start Chromium headless failed: %v", err)
	}
	go func() {
		cmd.Wait() // nolint:errcheck
	}()
	time.Sleep(3 * time.Second)
	defer func() {
		if err := cmd.Process.Kill(); err != nil {
			t.Errorf("Failed to kill process: %v", err)
		}
	}()

	input, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatal(err)
	}
	remote, err := NewChromeRemoteScreenshoter("127.0.0.1:9222")
	if err != nil {
		t.Fatal(err)
	}
	shot, err := remote.Screenshot(ctx, input, ScaleFactor(1))
	if err != nil {
		if err == context.DeadlineExceeded {
			t.Error(err.Error(), http.StatusRequestTimeout)
			return
		}
		t.Error(err.Error(), http.StatusServiceUnavailable)
		return
	}

	if reflect.TypeOf(shot) != reflect.TypeOf(&Screenshots{}) {
		t.Fatalf("Unexpected type of Screenshots")
	}

	wantTitle := "Example Domain"
	if shot.Title != wantTitle {
		t.Fatalf("Unexpected title of webpage, got %s instead of %s", shot.Title, wantTitle)
	}

	if shot.Image == nil {
		t.Fail()
	}
}

func TestScreenshotFormat(t *testing.T) {
	binPath := helper.FindChromeExecPath()
	if _, err := exec.LookPath(binPath); err != nil {
		t.Skip("Chrome headless browser no found, skipped")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	ts := newServer()
	defer ts.Close()

	input, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatal(err)
	}
	shot, err := Screenshot(ctx, input, Quality(100))
	if err != nil {
		t.Fatal(err.Error(), http.StatusServiceUnavailable)
	}
	if reflect.TypeOf(shot) != reflect.TypeOf(&Screenshots{}) {
		t.Fatalf("Unexpected type of Screenshots")
	}
	if shot.Title != "Example Domain" || shot.Image == nil {
		t.Fatalf("screenshots empty, title: %s, data: %v", shot.Title, shot.Image)
	}
	contentType := http.DetectContentType(shot.Image)
	if contentType != "image/png" {
		t.Fatalf("content type should be image/png, got: %s", contentType)
	}

	shot, err = Screenshot(ctx, input, Format("jpg"))
	if err != nil {
		t.Fatal(err.Error(), http.StatusServiceUnavailable)
	}
	if reflect.TypeOf(shot) != reflect.TypeOf(&Screenshots{}) {
		t.Fatalf("Unexpected type of Screenshots")
	}
	if shot.Title != "Example Domain" || shot.Image == nil {
		t.Fatalf("screenshots empty, title: %s, data: %v", shot.Title, shot.Image)
	}
	contentType = http.DetectContentType(shot.Image)
	if contentType != "image/jpeg" {
		t.Fatalf("content type should be image/jpeg, got: %s", contentType)
	}
}

func TestScreenshotAsPDF(t *testing.T) {
	binPath := helper.FindChromeExecPath()
	if _, err := exec.LookPath(binPath); err != nil {
		t.Skip("Chrome headless browser no found, skipped")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	ts := newServer()
	defer ts.Close()

	input, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatal(err)
	}
	shot, err := Screenshot(ctx, input, ScaleFactor(1), PrintPDF(true))
	if err != nil {
		if err == context.DeadlineExceeded {
			t.Error(err.Error(), http.StatusRequestTimeout)
			return
		}
		t.Error(err.Error(), http.StatusServiceUnavailable)
		return
	}

	if reflect.TypeOf(shot) != reflect.TypeOf(&Screenshots{}) {
		t.Fatalf("Unexpected type of Screenshots")
	}

	if shot.Title != "Example Domain" {
		t.Log(shot.Title)
		t.Fail()
	}

	if shot.Image == nil {
		t.Fail()
	}
	if shot.PDF == nil {
		t.Error("unexpected screenshot as pdf")
	}
}

func TestScreenshotAsHTML(t *testing.T) {
	binPath := helper.FindChromeExecPath()
	if _, err := exec.LookPath(binPath); err != nil {
		t.Skip("Chrome headless browser no found, skipped")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	ts := newServer()
	defer ts.Close()

	input, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatal(err)
	}
	shot, err := Screenshot(ctx, input, ScaleFactor(1), RawHTML(true))
	if err != nil {
		if err == context.DeadlineExceeded {
			t.Error(err.Error(), http.StatusRequestTimeout)
			return
		}
		t.Error(err.Error(), http.StatusServiceUnavailable)
		return
	}

	if reflect.TypeOf(shot) != reflect.TypeOf(&Screenshots{}) {
		t.Fatalf("Unexpected type of Screenshots")
	}

	wantTitle := "Example Domain"
	if shot.Title != wantTitle {
		t.Fatalf("Unexpected title of webpage, got %s instead of %s", shot.Title, wantTitle)
	}

	if shot.Image == nil {
		t.Fail()
	}
	if shot.HTML == nil {
		t.Error("unexpected screenshot as raw html")
	}
}

func TestScreenshotAsHAR(t *testing.T) {
	binPath := helper.FindChromeExecPath()
	if _, err := exec.LookPath(binPath); err != nil {
		t.Skip("Chrome headless browser no found, skipped")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	ts := newServer()
	defer ts.Close()

	input, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatal(err)
	}
	shot, err := Screenshot(ctx, input, ScaleFactor(1), DumpHAR(true))
	if err != nil {
		if err == context.DeadlineExceeded {
			t.Error(err.Error(), http.StatusRequestTimeout)
			return
		}
		t.Error(err.Error(), http.StatusServiceUnavailable)
		return
	}

	if reflect.TypeOf(shot) != reflect.TypeOf(&Screenshots{}) {
		t.Fatalf("Unexpected type of Screenshots")
	}

	wantTitle := "Example Domain"
	if shot.Title != wantTitle {
		t.Fatalf("Unexpected title of webpage, got %s instead of %s", shot.Title, wantTitle)
	}

	if shot.HAR == nil {
		t.Error("unexpected screenshot as HAR")
	}
}

func TestConvertURI(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/pdf")
		io.WriteString(w, "Fake Content") // nolint:errcheck
	}))

	inp, _ := url.Parse(server.URL)
	uri := convertURI(inp)
	if !strings.Contains(uri, "docs.google.com") {
		t.Errorf("unexpected convert document content viewer url got %s instead of %s", uri, "docs.google.com")
	}
}

func TestImportCookies(t *testing.T) {
	f := `cookies:
  example.com:
    - name: 'foo'
      value: 'bar'
      domain: 'example.com'
      path: '/'
      expires: '2022-08-12T11:57:14.005Z'
      size: 32
      httpOnly: true
      secure: true
      sameSite: 'Lax'
      sameParty: false
      priority: 'Medium'
    - name: 'zoo'
      value: 'zoo'
  example.org:
    - name: 'foo'
      value: 'bar'`
	cookies, err := ImportCookies([]byte(f))
	if err != nil {
		t.Fatal(err)
	}

	if exp, num := 3, len(cookies); num != exp {
		t.Fatalf("unexpected import cookies got number of cookies %d instead of %d", num, exp)
	}

	if exp, domain := "example", cookies[0].Domain; !strings.HasPrefix(domain, exp) {
		t.Errorf("unexpected import cookies got the first domain %s instead of %s", domain, exp)
	}

	if exp, domain := "example", cookies[1].Domain; !strings.HasPrefix(domain, exp) {
		t.Errorf("unexpected import cookies got the first domain %s instead of %s", domain, exp)
	}
}

func TestScreenshotWithCookies(t *testing.T) {
	binPath := helper.FindChromeExecPath()
	if _, err := exec.LookPath(binPath); err != nil {
		t.Skip("Chrome headless browser no found, skipped")
	}

	_, mux, server := helper.MockServer()
	mux.HandleFunc("/", func(res http.ResponseWriter, req *http.Request) {
		cookies := req.Cookies()
		if len(cookies) == 0 {
			http.Error(res, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		_, err := json.MarshalIndent(cookies, "", "  ")
		if err != nil {
			http.Error(res, err.Error(), http.StatusInternalServerError)
			return
		}
		if cookies[0].String() == "foo=bar" {
			fmt.Fprintf(res, "ok")
		} else {
			fmt.Fprintf(res, "")
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	ts := newServer()
	defer ts.Close()

	input, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}

	f := `cookies:
  localhost:
    - name: 'foo'
      value: 'bar'
      domain: '127.0.0.1'
      path: '/'
      expires: '2022-08-12T11:57:14.005Z'
      size: 32
      httpOnly: true
      secure: true
      sameSite: 'Lax'
      sameParty: false
      priority: 'Medium'`
	cookies, err := ImportCookies([]byte(f))
	if err != nil {
		t.Fatal(err)
	}

	shot, err := Screenshot(ctx, input, RawHTML(true), Cookies(cookies))
	if err != nil {
		if err == context.DeadlineExceeded {
			t.Error(err.Error(), http.StatusRequestTimeout)
			return
		}
		t.Error(err.Error(), http.StatusServiceUnavailable)
		return
	}

	html := `<html><head></head><body><pre style="word-wrap: break-word; white-space: pre-wrap;">ok</pre></body></html>`
	if exp, got := html, string(shot.HTML); exp != got {
		t.Errorf("unexpected screenshot with cookie got %s instead of %s", got, exp)
	}
}

func TestImportLocalStorage(t *testing.T) {
	f := `local-storage:
  example.com:
    - key: 'foo'
      value: 'bar'
      host: 'example.com'
    - key: 'foo'
      value: 'bar'`
	storage, err := ImportStorage([]byte(f))
	if err != nil {
		t.Fatal(err)
	}

	if exp, num := 2, len(storage); num != exp {
		t.Fatalf("unexpected import storage got number of storage %d instead of %d", num, exp)
	}

	if exp, host := "example.com", storage[0].Host; host != exp {
		t.Errorf("unexpected import storage got the first host %s instead of %s", host, exp)
	}

	if exp, host := "example.com", storage[1].Host; host != exp {
		t.Errorf("unexpected import storage got the first host %s instead of %s", host, exp)
	}
}

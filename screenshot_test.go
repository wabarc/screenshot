package screenshot

import (
	"context"
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

	if reflect.TypeOf(shot) != reflect.TypeOf(Screenshots{}) {
		t.Fail()
	}

	if shot.Title != "Example Domain" {
		t.Log(shot.Title)
		t.Fail()
	}

	if shot.Image == nil {
		t.Fail()
	}
}

func TestScreenshotWithRemote(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	ts := newServer()
	defer ts.Close()

	binPath := helper.FindChromeExecPath()
	if binPath == "" {
		t.Skip("Chrome headless browser no found, skipped")
	}

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

	if reflect.TypeOf(shot) != reflect.TypeOf(Screenshots{}) {
		t.Fail()
	}

	if shot.Title != "Example Domain" {
		t.Log(shot.Title)
		t.Fail()
	}

	if shot.Image == nil {
		t.Fail()
	}
}

func TestScreenshotFormat(t *testing.T) {
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
	if reflect.TypeOf(shot) != reflect.TypeOf(Screenshots{}) {
		t.Fail()
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
	if reflect.TypeOf(shot) != reflect.TypeOf(Screenshots{}) {
		t.Fail()
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

	if reflect.TypeOf(shot) != reflect.TypeOf(Screenshots{}) {
		t.Fail()
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

	if reflect.TypeOf(shot) != reflect.TypeOf(Screenshots{}) {
		t.Fail()
	}

	if shot.Title != "Example Domain" {
		t.Log(shot.Title)
		t.Fail()
	}

	if shot.Image == nil {
		t.Fail()
	}
	if shot.HTML == nil {
		t.Error("unexpected screenshot as raw html")
	}
}

func TestConvertURI(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/pdf")
		io.WriteString(w, "Fake Content") // nolint:errcheck
	}))

	uri := convertURI(server.URL)
	if !strings.Contains(uri, "docs.google.com") {
		t.Errorf("unexpected convert document content viewer url got %s instead of %s", uri, "docs.google.com")
	}
}

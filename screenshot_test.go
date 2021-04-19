package screenshot

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"reflect"
	"strings"
	"testing"
	"time"
)

func writeHTML(content string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		io.WriteString(w, strings.TrimSpace(content))
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

	urls := []string{ts.URL}
	shots, err := Screenshot(ctx, urls, ScaleFactor(1))
	if err != nil {
		t.Log(urls)
		if err == context.DeadlineExceeded {
			t.Error(err.Error(), http.StatusRequestTimeout)
			return
		}
		t.Error(err.Error(), http.StatusServiceUnavailable)
		return
	}

	for _, shot := range shots {
		if reflect.TypeOf(shot) != reflect.TypeOf(Screenshots{}) {
			t.Fail()
		}

		if shot.Title != "Example Domain" {
			t.Log(shot.Title)
			t.Fail()
		}

		if shot.Data == nil {
			t.Fail()
		}
	}
}

func TestScreenshotWithRemote(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	ts := newServer()
	defer ts.Close()

	cmd := exec.Command("chromium-browser", "--headless", "--disable-gpu", "--no-sandbox", "--remote-debugging-port=9222")
	if err := cmd.Start(); err != nil {
		t.Fatalf("Start Chromium headless failed: %v", err)
	}
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
		select {
		case <-time.After(60 * time.Second):
			if err := cmd.Process.Kill(); err != nil {
				t.Errorf("Failed to kill process: %v", err)
			}
			t.Log("Process killed as timeout reached")
		case err := <-done:
			if err != nil {
				t.Errorf("Process finished with error: %v", err)
			}
			if err := cmd.Process.Kill(); err != nil {
				t.Errorf("Failed to kill process: %v", err)
			}
			t.Log("Process finished successfully")
		}
	}()
	time.Sleep(3 * time.Second)

	urls := []string{ts.URL}
	remote, err := NewChromeRemoteScreenshoter("127.0.0.1:9222")
	if err != nil {
		t.Fatal(err)
	}
	shots, err := remote.Screenshot(ctx, urls, ScaleFactor(1))
	if err != nil {
		t.Log(urls)
		if err == context.DeadlineExceeded {
			t.Error(err.Error(), http.StatusRequestTimeout)
			done <- err
			return
		}
		t.Error(err.Error(), http.StatusServiceUnavailable)
		done <- err
		return
	}

	for _, shot := range shots {
		if reflect.TypeOf(shot) != reflect.TypeOf(Screenshots{}) {
			t.Fail()
		}

		if shot.Title != "Example Domain" {
			t.Log(shot.Title)
			t.Fail()
		}

		if shot.Data == nil {
			t.Fail()
		}
	}
	done <- nil
}

func TestScreenshotFormat(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	ts := newServer()
	defer ts.Close()

	urls := []string{ts.URL}
	shots, err := Screenshot(ctx, urls, Format("png"))
	if err != nil {
		t.Fatal(err.Error(), http.StatusServiceUnavailable)
	}
	shot := shots[0]
	if reflect.TypeOf(shot) != reflect.TypeOf(Screenshots{}) {
		t.Fail()
	}
	if shot.Title != "Example Domain" || shot.Data == nil {
		t.Fatalf("screenshots empty, title: %s, data: %v", shot.Title, shot.Data)
	}
	contentType := http.DetectContentType(shot.Data)
	if contentType != "image/png" {
		t.Fatalf("content type should be image/png, got: %s", contentType)
	}

	shots, err = Screenshot(ctx, urls, Format("jpg"))
	if err != nil {
		t.Fatal(err.Error(), http.StatusServiceUnavailable)
	}
	shot = shots[0]
	if reflect.TypeOf(shot) != reflect.TypeOf(Screenshots{}) {
		t.Fail()
	}
	if shot.Title != "Example Domain" || shot.Data == nil {
		t.Fatalf("screenshots empty, title: %s, data: %v", shot.Title, shot.Data)
	}
	contentType = http.DetectContentType(shot.Data)
	if contentType != "image/jpeg" {
		t.Fatalf("content type should be image/jpeg, got: %s", contentType)
	}
}

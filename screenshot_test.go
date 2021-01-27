package screenshot

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
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

func TestScreenshot(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	ts := httptest.NewServer(writeHTML(`
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
	defer ts.Close()

	urls := []string{ts.URL}
	shots, err := Screenshot(ctx, urls, ScaleFactorScreenshotOption(1))
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

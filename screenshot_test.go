package screenshot

import (
	"context"
	"net/http"
	"testing"
	"time"
)

func TestScreenshot(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	url := []string{"https://www.example.org/"}
	buf, err := Screenshot(ctx, url, ScaleFactorScreenshotOption(1))
	if err != nil {
		if err == context.DeadlineExceeded {
			t.Error(err.Error(), http.StatusRequestTimeout)
			return
		}
		t.Error(err.Error(), http.StatusServiceUnavailable)
		return
	}

	for _, b := range buf {
		if b == nil {
			t.Fail()
		}
	}
}

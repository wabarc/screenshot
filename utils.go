// Copyright 2021 Wayback Archiver. All rights reserved.
// Use of this source code is governed by the GNU GPL v3
// license that can be found in the LICENSE file.

package screenshot // import "github.com/wabarc/screenshot"

import (
	"mime"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

func viewerEndpoint() string {
	return "https://docs.google.com/viewer?url="
}

func convertURI(u *url.URL) string {
	client := http.Client{Timeout: 3 * time.Second}
	resp, err := client.Head(u.String())
	if err != nil {
		return u.String()
	}
	resp.Body.Close()

	// see: https://gist.github.com/tzmartin/1cf85dc3d975f94cfddc04bc0dd399be
	// Common MIME types
	// https://developer.mozilla.org/en-US/docs/Web/HTTP/Basics_of_HTTP/MIME_types/Common_types
	contentType := resp.Header.Get("Content-Type")
	t, _, _ := mime.ParseMediaType(contentType)
	switch t {
	case "application/pdf", "application/msword",
		"application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		"application/vnd.ms-excel", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
		"application/vnd.ms-powerpoint", "application/vnd.openxmlformats-officedocument.presentationml.presentation":
		return viewerEndpoint() + u.String()
	}

	return u.String()
}

func revertURI(link string) string {
	return strings.Replace(link, viewerEndpoint(), "", 1)
}

func proxyServer() string {
	// https://www.chromium.org/developers/design-documents/network-stack/socks-proxy/
	var server string
	keys := []string{
		"PROXY_SERVER",
		"HTTP_PROXY",
		"HTTPS_PROXY",
		"ALL_PROXY",
		"http_proxy",
		"https_proxy",
		"all_proxy",
	}
	for _, key := range keys {
		if server = os.Getenv(key); server != "" {
			if _, err := url.Parse(server); err != nil {
				continue
			} else {
				break
			}
		}
	}

	return server
}

func wsURLReadTimeout() time.Duration {
	timeout := os.Getenv("CHROMEDP_WSURLREADTIMEOUT")
	defaultTimeout := 20 * time.Second
	if timeout == "" {
		return defaultTimeout
	}

	i, err := strconv.Atoi(timeout)
	if err != nil {
		return defaultTimeout
	}

	return time.Duration(i) * time.Second
}

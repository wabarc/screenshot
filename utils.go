// Copyright 2021 Wayback Archiver. All rights reserved.
// Use of this source code is governed by the GNU GPL v3
// license that can be found in the LICENSE file.

package screenshot // import "github.com/wabarc/screenshot"

import (
	"mime"
	"net/http"
	"net/url"
)

func convertURI(link string) string {
	uri, err := url.Parse(link)
	if err != nil {
		return link
	}

	resp, err := http.Head(uri.String())
	if err != nil {
		return link
	}
	resp.Body.Close()

	// see: https://gist.github.com/tzmartin/1cf85dc3d975f94cfddc04bc0dd399be
	// Common MIME types
	// https://developer.mozilla.org/en-US/docs/Web/HTTP/Basics_of_HTTP/MIME_types/Common_types
	viewer := "https://docs.google.com/viewer?url="
	contentType := resp.Header.Get("Content-Type")
	t, _, _ := mime.ParseMediaType(contentType)
	switch t {
	case "application/pdf", "application/msword",
		"application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		"application/vnd.ms-excel", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
		"application/vnd.ms-powerpoint", "application/vnd.openxmlformats-officedocument.presentationml.presentation":
		return viewer + uri.String()
	}

	return link
}

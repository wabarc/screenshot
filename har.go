// Copyright 2021 Wayback Archiver. All rights reserved.
// Use of this source code is governed by the GNU GPL v3
// license that can be found in the LICENSE file.

package screenshot // import "github.com/wabarc/screenshot"

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/chromedp/cdproto/har"
	"github.com/chromedp/cdproto/network"
	"github.com/pkg/errors"
)

// copied from https://github.com/chromedp/chromedp/issues/42#issuecomment-500191682
type hRequest har.Request
type hResponse har.Response

type creator struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Comment string `json:"commment"`
}

type pageTimings struct {
	OnContentLoad float64 `json:"onContentLoad,omitempty"`
	OnLoad        float64 `json:"onLoad,omitempty"`
}

type hpage struct {
	ID              string      `json:"id,omitempty"`
	Title           string      `json:"title,omitempty"`
	StartedDateTime string      `json:"startedDateTime,omitempty"`
	PageTimings     pageTimings `json:"pageTimings,omitempty"`
}

type entry struct {
	Pageref         string      `json:"pageref,omitempty"`
	StartedDateTime string      `json:"startedDateTime"`
	Time            float64     `json:"time"`
	Request         *hRequest   `json:"request"`
	Response        *hResponse  `json:"response"`
	Cache           interface{} `json:"cache"`
	Timings         interface{} `json:"timings"`
	ServerIPAddress string      `json:"serverIPAddress,omitempty"`
	Connection      string      `json:"connection,omitempty"`
	Comment         string      `json:"comment,omitempty"`
}

type browser struct {
	Name    string
	Version string
	Comment string
}

type hlog struct {
	Version string  `json:"version"`
	Creator creator `json:"creator"`
	Browser browser `json:"browser,omitempty"`
	Pages   []hpage `json:"pages,omitempty"`
	Entries []entry `json:"entries"`
	Comment string  `json:"comment,omitempty"`
}

type HAR struct {
	Log hlog `json:"log,omitempty"`
}

var start = time.Now()

// process requests and return a structured data
func processRequest(r *network.EventRequestWillBeSent, cookies []*network.Cookie, options ScreenshotOptions) *hRequest {
	req := hRequest{}
	if !options.DumpHAR {
		return &req
	}

	// http method
	req.Method = r.Request.Method
	// http request url
	req.URL = r.Request.URL
	// http version.
	req.HTTPVersion = ""
	// Associated headers for the request.
	req.Headers = []*har.NameValuePair{}
	// headers from the *network.EventRequestWillBeSent are in the form,
	// map[key:value]. this needs to be converted to the form of a
	// har.NameValuePair
	for header := range r.Request.Headers {
		h := har.NameValuePair{}
		h.Name = header
		h.Value = r.Request.Headers[header].(string)
		req.Headers = append(req.Headers, &h)
	}
	// Store cookie details.
	req.Cookies = []*har.Cookie{}
	// Url Query stirngs details.
	req.QueryString = []*har.NameValuePair{}
	u, err := url.Parse(req.URL)
	if err != nil {
		return &req
	}
	// Query strings are of the format name = []values when
	// received from the network.EventRequestWillBeSent. This
	// needs to be converted to the form of multiple name, value
	// pairs.
	for name := range u.Query() {
		if len(name) != 0 {
			values := u.Query()[name]
			for _, val := range values {
				req.QueryString = append(req.QueryString, &har.NameValuePair{
					Name:  name,
					Value: val,
				})
			}
		}
	}
	// req.Postdata points to the post data.
	req.PostData = nil
	//if req.Method == "POST" {
	//// Process the post data of the form *har.PostData
	//}
	// TODO : to implement headersize and bodySize for the request
	req.HeadersSize = 0
	req.BodySize = 0
	return &req
}

func processResponse(r *network.EventResponseReceived, cookies []*network.Cookie, body []byte, options ScreenshotOptions) *hResponse {
	res := hResponse{}
	if !options.DumpHAR {
		return &res
	}

	res.Status = r.Response.Status
	res.StatusText = http.StatusText(int(r.Response.Status))
	res.HTTPVersion = r.Response.Protocol
	// TODO : implement cookie information.
	res.Cookies = []*har.Cookie{}
	for _, c := range cookies {
		hc := &har.Cookie{
			Name:     c.Name,
			Value:    c.Value,
			Path:     c.Path,
			Domain:   c.Domain,
			Expires:  fmt.Sprint(c.Expires),
			HTTPOnly: c.HTTPOnly,
			Secure:   c.Secure,
			Comment:  "",
		}
		res.Cookies = append(res.Cookies, hc)
	}
	res.Headers = []*har.NameValuePair{}
	// headers from the *network.EventRequestWillBeSent are in the form,
	// map[key:value]. this needs to be converted to the form of a
	// har.NameValuePair
	for header := range r.Response.Headers {
		h := har.NameValuePair{}
		h.Name = header
		h.Value = r.Response.Headers[header].(string)
		res.Headers = append(res.Headers, &h)
	}
	// response content
	res.Content = &har.Content{}
	res.Content.MimeType = r.Response.MimeType
	res.Content.Size = int64(r.Response.EncodedDataLength)
	res.Content.Text = base64.StdEncoding.EncodeToString(body)
	if res.Content.Text != "" {
		res.Content.Encoding = "base64"
	}

	// Redirect URL
	res.RedirectURL = ""
	res.HeadersSize = 0
	res.BodySize = 0

	return &res
}

func compose(requestsID []network.RequestID, mRequests, mResponses *sync.Map, options ScreenshotOptions, uri string) (buf []byte, err error) {
	if !options.DumpHAR {
		return buf, err
	}

	pageID := "page_1"
	var entries []entry
	for reqID := range requestsID {
		vreq, ok := mRequests.Load(requestsID[reqID])
		if !ok {
			continue
		}
		vres, ok := mResponses.Load(requestsID[reqID])
		if !ok {
			continue
		}
		entries = append(entries, entry{
			Pageref:         pageID,
			StartedDateTime: start.Format(time.RFC3339Nano),
			Time:            0,
			Request:         vreq.(*hRequest),
			Response:        vres.(*hResponse),
			// Cache: ,
			// Timings: ,
			// ServerIPAddress: ,
			// Connection: ,
			// Comment: ,
		})
	}

	har := HAR{
		Log: hlog{
			Version: "1.2",
			Creator: creator{
				Name:    "Wayback Archiver",
				Version: "latest",
				Comment: "https://github.com/wabarc",
			},
			Pages: []hpage{
				hpage{
					ID:              pageID,
					Title:           uri,
					StartedDateTime: start.Format(time.RFC3339Nano),
					PageTimings: pageTimings{
						OnContentLoad: -1,
						OnLoad:        -1,
					},
				},
			},
			Entries: entries,
		},
	}

	buf, e := json.MarshalIndent(har, "", "  ")
	if e != nil {
		return nil, errors.Wrap(err, e.Error())
	}

	return buf, err
}

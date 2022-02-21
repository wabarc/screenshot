package screenshot

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/dom"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
	"github.com/pkg/errors"
	"github.com/wabarc/helper"
	"github.com/wabarc/logger"
	"gopkg.in/yaml.v2"
)

func init() {
	debug := os.Getenv("DEBUG")
	if debug == "true" || debug == "1" || debug == "on" {
		logger.EnableDebug()
	}
}

type Screenshots struct {
	URL   string
	Title string
	Image []byte
	HTML  []byte
	PDF   []byte
	HAR   []byte
}

// Screenshoter is a webpage screenshot interface.
type Screenshoter interface {
	Screenshot(ctx context.Context, input *url.URL, options ...ScreenshotOption) (Screenshots, error)
}

type chromeRemoteScreenshoter struct {
	url string
}

// NewChromeRemoteScreenshoter creates a Screenshoter backed by Chrome DevTools Protocol.
// The addr is the headless chrome websocket debugger endpoint, such as 127.0.0.1:9222.
func NewChromeRemoteScreenshoter(addr string) (Screenshoter, error) {
	// Due to issue#505 (https://github.com/chromedp/chromedp/issues/505),
	// chrome restricts the host must be IP or localhost, we should rewrite the url.
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s/json/version", addr), nil)
	if err != nil {
		return nil, err
	}
	req.Host = "localhost"
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	if err = json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return &chromeRemoteScreenshoter{
		url: strings.Replace(result["webSocketDebuggerUrl"].(string), "localhost", addr, 1),
	}, nil
}

func (s *chromeRemoteScreenshoter) Screenshot(ctx context.Context, input *url.URL, options ...ScreenshotOption) (shot Screenshots, err error) {
	if s.url == "" {
		return shot, fmt.Errorf("can't connect to headless browser")
	}

	ctx, cancel := chromedp.NewRemoteAllocator(ctx, s.url)
	defer cancel()

	return screenshotStart(ctx, input, options...)
}

func Screenshot(ctx context.Context, input *url.URL, options ...ScreenshotOption) (shot Screenshots, err error) {
	if _, err := exec.LookPath(helper.FindChromeExecPath()); err != nil {
		return shot, err
	}

	// https://github.com/chromedp/chromedp/blob/b56cd66f9cebd6a1fa1283847bbf507409d48225/allocate.go#L53
	var allocOpts = append(
		chromedp.DefaultExecAllocatorOptions[:],
		// chromedp.CombinedOutput(log.Writer()),
		chromedp.NoDefaultBrowserCheck,
		chromedp.Flag("ignore-certificate-errors", true),
		chromedp.Flag("allow-running-insecure-content", true),
		chromedp.Flag("no-default-browser-check", true),
		chromedp.Flag("disable-web-security", true),
		chromedp.Flag("disable-webgl", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("no-first-run", true),
	)
	f := "false"
	if noHeadless := os.Getenv("CHROMEDP_NO_HEADLESS"); noHeadless != "" && noHeadless != f {
		allocOpts = append(allocOpts, chromedp.Flag("headless", false))
	}
	if noSandbox := os.Getenv("CHROMEDP_NO_SANDBOX"); noSandbox != "" && noSandbox != f {
		allocOpts = append(allocOpts, chromedp.NoSandbox)
	}
	if disableGPU := os.Getenv("CHROMEDP_DISABLE_GPU"); disableGPU != "" && disableGPU != f {
		allocOpts = append(allocOpts, chromedp.DisableGPU)
	}
	if userAgent := os.Getenv("CHROMEDP_USER_AGENT"); userAgent != "" {
		allocOpts = append(allocOpts, chromedp.UserAgent(userAgent))
	}
	dir, err := ioutil.TempDir(os.TempDir(), "chromedp-runner-*")
	if err == nil && dir != "" {
		defer os.RemoveAll(dir)
		allocOpts = append(allocOpts, chromedp.UserDataDir(dir))
	}
	ctx, cancel := chromedp.NewExecAllocator(ctx, allocOpts...)
	defer cancel()

	return screenshotStart(ctx, input, options...)
}

func screenshotStart(ctx context.Context, input *url.URL, options ...ScreenshotOption) (shot Screenshots, err error) {
	var browserOpts []chromedp.ContextOption
	if debug := os.Getenv("CHROMEDP_DEBUG"); debug != "" && debug != "false" {
		browserOpts = append(browserOpts, chromedp.WithDebugf(log.Printf))
	}
	ctx, cancel := chromedp.NewContext(ctx, browserOpts...)
	defer cancel()

	var opts ScreenshotOptions
	for _, o := range options {
		o(&opts)
	}
	if opts.Quality != 100 {
		opts.Format = page.CaptureScreenshotFormatJpeg
	}

	// run a no-op action to allocate the browser
	// if err := chromedp.Run(ctx, chromedp.ActionFunc(func(_ context.Context) error {
	// 	return nil
	// })); err != nil {
	// 	return nil, err
	// }

	url := convertURI(input)
	var buf []byte
	var pdf []byte
	var har []byte
	var raw string
	var title string

	nRequests := &sync.Map{}
	nResponses := &sync.Map{}
	requestsID := []network.RequestID{}
	wg := sync.WaitGroup{}
	mu := sync.Mutex{}
	chromedp.ListenTarget(ctx, func(v interface{}) {
		switch v := v.(type) {
		case *page.EventJavascriptDialogOpening:
			go func() {
				ctx, cancel := context.WithTimeout(ctx, time.Second)
				defer cancel()
				_ = chromedp.Run(ctx, page.HandleJavaScriptDialog(true))
			}()
		case *network.EventRequestWillBeSent:
			wg.Add(1)
			go func(r *network.EventRequestWillBeSent) {
				defer wg.Done()
				var cookies []*network.Cookie
				req := processRequest(r, cookies, opts)
				nRequests.Store(r.RequestID, req)
				mu.Lock()
				requestsID = append(requestsID, r.RequestID)
				mu.Unlock()
			}(v)
		case *network.EventResponseReceived:
			wg.Add(1)
			go func(r *network.EventResponseReceived) {
				defer wg.Done()
				var body []byte
				var ids []cdp.NodeID
				var cookies []*network.Cookie
				ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
				defer cancel()
				mu.Lock()
				_ = chromedp.Run(ctx,
					chromedp.NodeIDs(`document`, &ids, chromedp.ByJSPath),
					chromedp.ActionFunc(func(ctx context.Context) error {
						body, err = network.GetResponseBody(r.RequestID).Do(ctx)
						return err
					}),
					chromedp.ActionFunc(func(ctx context.Context) error {
						cookies, err = network.GetAllCookies().Do(ctx)
						return err
					}),
				)
				mu.Unlock()
				res := processResponse(r, cookies, body, opts)
				nResponses.Store(r.RequestID, res)
			}(v)
		case *network.EventDataReceived:
			// Fired when data chunk was received over the network.
			// go func() {
			// 	edr := v.(*network.EventDataReceived)
			// 	fmt.Printf("%#v\n", edr)
			// }()
			// case *network.EventLoadingFinished:
			// 	go func() {
			// 		lf := v.(*network.EventLoadingFinished)
			// 	}()
			// case *network.EventLoadingFailed:
			// 	// Fired when HTTP request has failed to load.
			// 	go func() {
			// 		lf := v.(*network.EventLoadingFailed)
			// 	}()
		}
	})

	captureAction := screenshotAction(&buf, opts)
	exportHTML := exportHTML(&raw, opts)
	saveAsPDF := printPDF(&pdf, opts)
	if err := chromedp.Run(ctx, chromedp.Tasks{
		page.Enable(),
		network.Enable(),
		// enableLifeCycleEvents(),
		stealth(),
		setCookies(opts),
		setLocalStorage(input, opts),
		page.SetDownloadBehavior(page.SetDownloadBehaviorBehaviorDeny),
		navigateAndWaitFor(url, "DOMContentLoaded"),
		// chromedp.Navigate(url),
		chromedp.WaitReady("body"),
		chromedp.Sleep(time.Second),
		scrollToBottom(),
		chromedp.Title(&title),
		captureAction,
		exportHTML,
		saveAsPDF,
		chromedp.ResetViewport(),
		chromedp.Sleep(time.Second),
		closePageAction(),
	}); err != nil {
		log.Print(err)
		buf = nil
	}

	// Wait for all the go routines to complete
	wg.Wait()

	var html []byte
	if raw != "" {
		html = helper.String2Byte(raw)
	}
	har, _ = compose(requestsID, nRequests, nResponses, opts, url)
	shot = Screenshots{
		URL:   revertURI(url),
		PDF:   pdf,
		HAR:   har,
		HTML:  html,
		Image: buf,
		Title: title,
	}

	return shot, nil
}

// https://github.com/chromedp/chromedp/blob/875d6f4a3453149639d7fa83a2fa473b743fc33f/poll.go#L88-L127
func scrollToBottom() chromedp.Action {
	const script = `()=>{
    let distance = 150;
    let scrollHeight = document.documentElement.scrollHeight || document.body.scrollHeight;
    let currentHeight = window.innerHeight + window.pageYOffset;
    window.scrollBy(0, distance);
    if (currentHeight >= scrollHeight) {
        return true;
    }
    return false;
}`

	// Scroll down to the bottom line by line
	return chromedp.Tasks{
		chromedp.PollFunction(script, nil, chromedp.WithPollingInterval(150*time.Millisecond)),
	}
}

func setCookies(options ScreenshotOptions) chromedp.Action {
	if len(options.Cookies) == 0 {
		return chromedp.Tasks{}
	}

	return chromedp.Tasks{
		chromedp.ActionFunc(func(ctx context.Context) (err error) {
			for key := range options.Cookies {
				cookie := options.Cookies[key]
				expire := cdp.TimeSinceEpoch(cookie.Expires)
				er := network.SetCookie(cookie.Name, cookie.Value).
					WithDomain(cookie.Domain).
					WithPath(cookie.Path).
					WithExpires(&expire).
					WithHTTPOnly(cookie.HTTPOnly).
					WithSecure(cookie.Secure).
					WithSameParty(cookie.SameParty).
					WithSameSite(network.CookieSameSite(cookie.SameSite)).
					WithPriority(network.CookiePriority(cookie.Priority)).
					Do(ctx)
				if er != nil {
					err = errors.Wrap(err, er.Error())
				}
			}
			return err
		}),
	}
}

func setLocalStorage(u *url.URL, options ScreenshotOptions) chromedp.Action {
	if len(options.Storage) == 0 {
		return chromedp.Tasks{}
	}

	return chromedp.Tasks{
		chromedp.ActionFunc(func(ctx context.Context) (err error) {
			for key := range options.Storage {
				item := options.Storage[key]
				if u.Host != item.Host {
					continue
				}
				_, exp, er := runtime.Evaluate(fmt.Sprintf(`window.localStorage.setItem('%s', '%s')`, item.Key, item.Value)).Do(ctx)
				if er != nil {
					err = errors.Wrap(err, er.Error())
				}
				if exp != nil {
					err = errors.Wrap(err, exp.Error())
				}
			}
			return err
		}),
	}
}

// Note: this will override the viewport emulation settings.
func screenshotAction(res *[]byte, options ScreenshotOptions) chromedp.Action {
	return chromedp.Tasks{
		chromedp.ActionFunc(func(ctx context.Context) (err error) {
			// get layout metrics
			_, _, contentSize, _, _, cssContentSize, err := page.GetLayoutMetrics().Do(ctx)
			if err != nil {
				return err
			}
			if cssContentSize != nil {
				contentSize = cssContentSize
			}

			// Limit dimensions
			if options.MaxHeight > 0 && contentSize.Height > float64(options.MaxHeight) {
				contentSize.Height = float64(options.MaxHeight)
			}
			if options.MaxWidth > 0 && contentSize.Width > float64(options.MaxWidth) {
				contentSize.Width = float64(options.MaxWidth)
			}

			*res, err = page.CaptureScreenshot().
				WithCaptureBeyondViewport(true).
				WithQuality(options.Quality).
				WithFormat(options.Format).
				WithClip(&page.Viewport{
					X:      0,
					Y:      0,
					Width:  contentSize.Width,
					Height: contentSize.Height,
					Scale:  1,
				}).
				Do(ctx)
			if err != nil {
				return err
			}
			return nil
		}),
	}
}

func printPDF(res *[]byte, options ScreenshotOptions) chromedp.Action {
	if !options.PrintPDF {
		return chromedp.Tasks{}
	}

	return chromedp.Tasks{
		chromedp.ActionFunc(func(ctx context.Context) error {
			var err error
			*res, _, err = page.PrintToPDF().WithLandscape(true).WithPrintBackground(true).Do(ctx)
			return err
		}),
	}
}

func exportHTML(res *string, options ScreenshotOptions) chromedp.Action {
	if !options.RawHTML {
		return chromedp.Tasks{}
	}

	return chromedp.Tasks{
		// chromedp.OuterHTML("html", res, chromedp.ByQuery),
		chromedp.ActionFunc(func(ctx context.Context) error {
			node, err := dom.GetDocument().Do(ctx)
			if err != nil {
				return err
			}
			*res, err = dom.GetOuterHTML().WithNodeID(node.NodeID).Do(ctx)

			return err
		}),
	}
}

func closePageAction() chromedp.Action {
	return chromedp.ActionFunc(func(ctx context.Context) (err error) {
		return page.Close().Do(ctx)
	})
}

// page.SetLifecycleEventsEnabled is called by chromedp from v0.5.4
// https://github.com/chromedp/chromedp/issues/431#issuecomment-840433914
// nolint:deadcode,unused
func enableLifeCycleEvents() chromedp.ActionFunc {
	return func(ctx context.Context) error {
		err := page.Enable().Do(ctx)
		if err != nil {
			return err
		}
		err = page.SetLifecycleEventsEnabled(true).Do(ctx)
		if err != nil {
			return err
		}
		return nil
	}
}

func navigateAndWaitFor(url string, eventName string) chromedp.ActionFunc {
	return func(ctx context.Context) error {
		_, _, _, err := page.Navigate(url).Do(ctx)
		if err != nil {
			return err
		}

		return waitFor(ctx, eventName)
	}
}

// waitFor blocks until eventName is received.
// Examples of events you can wait for:
//     init, DOMContentLoaded, firstPaint,
//     firstContentfulPaint, firstImagePaint,
//     firstMeaningfulPaintCandidate,
//     load, networkAlmostIdle, firstMeaningfulPaint, networkIdle
//
// This is not super reliable, I've already found incidental cases where
// networkIdle was sent before load. It's probably smart to see how
// puppeteer implements this exactly.
func waitFor(ctx context.Context, eventName string) error {
	ch := make(chan struct{})
	cctx, cancel := context.WithCancel(ctx)
	chromedp.ListenTarget(cctx, func(ev interface{}) {
		switch e := ev.(type) {
		case *page.EventLifecycleEvent:
			if e.Name == eventName {
				cancel()
				close(ch)
			}
		default:
		}
	})
	select {
	case <-ch:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// ScreenshotOptions is the options used by Screenshot.
type ScreenshotOptions struct {
	Width  int64
	Height int64
	Mobile bool
	Format page.CaptureScreenshotFormat // jpg, png, default: png.

	Quality   int64
	MaxWidth  int64
	MaxHeight int64

	ScaleFactor float64

	PrintPDF bool
	RawHTML  bool
	DumpHAR  bool

	Cookies []Cookie
	Storage []LocalStorage
}

type ScreenshotOption func(*ScreenshotOptions)

func Width(width int64) ScreenshotOption {
	return func(opts *ScreenshotOptions) {
		opts.Width = width
	}
}

func Height(height int64) ScreenshotOption {
	return func(opts *ScreenshotOptions) {
		opts.Height = height
	}
}

func Quality(quality int64) ScreenshotOption {
	return func(opts *ScreenshotOptions) {
		opts.Quality = quality
	}
}

func MaxWidth(width int64) ScreenshotOption {
	return func(opts *ScreenshotOptions) {
		opts.MaxWidth = width
	}
}

func MaxHeight(height int64) ScreenshotOption {
	return func(opts *ScreenshotOptions) {
		opts.MaxHeight = height
	}
}

func ScaleFactor(factor float64) ScreenshotOption {
	return func(opts *ScreenshotOptions) {
		opts.ScaleFactor = factor
	}
}

func Mobile(b bool) ScreenshotOption {
	return func(opts *ScreenshotOptions) {
		opts.Mobile = b
	}
}

func Format(format string) ScreenshotOption {
	return func(opts *ScreenshotOptions) {
		switch format {
		default:
		case "png":
			opts.Format = page.CaptureScreenshotFormatPng
		case "jpg", "jpeg":
			opts.Format = page.CaptureScreenshotFormatJpeg
		}
	}
}

func PrintPDF(b bool) ScreenshotOption {
	return func(opts *ScreenshotOptions) {
		opts.PrintPDF = b
	}
}

func RawHTML(b bool) ScreenshotOption {
	return func(opts *ScreenshotOptions) {
		opts.RawHTML = b
	}
}

func DumpHAR(b bool) ScreenshotOption {
	return func(opts *ScreenshotOptions) {
		opts.DumpHAR = b
	}
}

// Cookie represents a cookie item.
type Cookie struct {
	Name      string    `yaml:"name"`                // Cookie name.
	Value     string    `yaml:"value"`               // Cookie value.
	Domain    string    `yaml:"domain"`              // Cookie domain.
	Path      string    `yaml:"path,omitempty"`      // Cookie path.
	Expires   time.Time `yaml:"expires,omitempty"`   // Cookie expiration date, session cookie if not set
	Size      int       `yaml:"size,omitempty"`      // Cookie size.
	HTTPOnly  bool      `yaml:"httpOnly,omitempty"`  // True if cookie is http-only.
	Secure    bool      `yaml:"secure,omitempty"`    // True if cookie is secure.
	SameSite  string    `yaml:"sameSite,omitempty"`  // Cookie SameSite type.
	SameParty bool      `yaml:"sameParty,omitempty"` // True if cookie is SameParty.
	Priority  string    `yaml:"priority,omitempty"`  // Cookie Priority type.
}

// ImportCookies imports cookies by given byte with yaml configuration.
// Format:
// cookies:
//   example.com:
//     - name: 'foo'
//       value: 'bar'
//     - name: 'zoo'
//       value: 'zoo'
//   example.org:
//     - name: 'foo'
//       value: 'bar'
func ImportCookies(r []byte) (cookies []Cookie, err error) {
	type configs struct {
		Cookies map[string][]Cookie `yaml:"cookies"`
	}
	var cfg configs
	if err := yaml.Unmarshal(r, &cfg); err != nil {
		return nil, err
	}
	for domain, items := range cfg.Cookies {
		for i := range items {
			if items[i].Domain == "" {
				items[i].Domain = domain
			}
			cookies = append(cookies, items[i])
		}
	}
	return cookies, nil
}

func Cookies(cookies []Cookie) ScreenshotOption {
	return func(opts *ScreenshotOptions) {
		opts.Cookies = cookies
	}
}

// LocalStorage represents a local storage item.
type LocalStorage struct {
	Key   string
	Value string
	Host  string
}

func ImportStorage(r []byte) (storage []LocalStorage, err error) {
	type configs struct {
		LocalStorage map[string][]LocalStorage `yaml:"local-storage"`
	}
	var cfg configs
	if err := yaml.Unmarshal(r, &cfg); err != nil {
		return nil, err
	}
	for domain, items := range cfg.LocalStorage {
		for i := range items {
			if items[i].Host == "" {
				items[i].Host = domain
			}
			storage = append(storage, items[i])
		}
	}
	return storage, nil
}

func Storage(storage []LocalStorage) ScreenshotOption {
	return func(opts *ScreenshotOptions) {
		opts.Storage = storage
	}
}

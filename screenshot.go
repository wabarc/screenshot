package screenshot

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
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

const defaultUA = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/108.0.0.0 Safari/537.36"

func init() {
	debug := os.Getenv("DEBUG")
	if debug == "true" || debug == "1" || debug == "on" {
		logger.EnableDebug()
	}
}

const perm = 0o644

type Byte []byte
type Path string

type As interface {
	Byte | Path
	String() string
}

func (b Byte) String() string {
	return helper.Byte2String(b)
}

func (p Path) String() string {
	return string(p)
}

type Screenshots[T As] struct {
	URL   string
	Title string
	Image T
	HTML  T
	PDF   T
	HAR   T

	// Total bytes of resources
	DataLength int64
}

// Screenshoter is a webpage screenshot interface.
type Screenshoter[T As] interface {
	Screenshot(ctx context.Context, input *url.URL, options ...ScreenshotOption) (*Screenshots[T], error)
}

type chromeRemoteScreenshoter[T As] struct {
	url string
}

// NewChromeRemoteScreenshoter creates a Screenshoter backed by Chrome DevTools Protocol.
// The addr is the headless chrome websocket debugger endpoint, such as 127.0.0.1:9222.
func NewChromeRemoteScreenshoter[T As](addr string) (res Screenshoter[T], err error) {
	// Due to issue#505 (https://github.com/chromedp/chromedp/issues/505),
	// chrome restricts the host must be IP or localhost, we should rewrite the url.
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s/json/version", addr), nil)
	if err != nil {
		return res, err
	}
	req.Host = "localhost"
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return res, err
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	if err = json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return res, err
	}

	res = &chromeRemoteScreenshoter[T]{
		url: strings.Replace(result["webSocketDebuggerUrl"].(string), "localhost", addr, 1),
	}
	return res, nil
}

func (s *chromeRemoteScreenshoter[T]) Screenshot(ctx context.Context, input *url.URL, options ...ScreenshotOption) (*Screenshots[T], error) {
	if s.url == "" {
		return nil, fmt.Errorf("can't connect to headless browser")
	}

	ctx, cancel := chromedp.NewRemoteAllocator(ctx, s.url)
	defer cancel()

	return screenshotStart[T](ctx, input, options...)
}

func Screenshot[T As](ctx context.Context, input *url.URL, options ...ScreenshotOption) (*Screenshots[T], error) {
	if _, err := exec.LookPath(helper.FindChromeExecPath()); err != nil {
		return nil, err
	}

	// https://github.com/chromedp/chromedp/blob/b56cd66f9cebd6a1fa1283847bbf507409d48225/allocate.go#L53
	var allocOpts = append(
		chromedp.DefaultExecAllocatorOptions[:],
		// chromedp.CombinedOutput(log.Writer()),
		chromedp.NoDefaultBrowserCheck,
		chromedp.Flag("ignore-certificate-errors", true),
		chromedp.Flag("allow-running-insecure-content", true),
		chromedp.Flag("no-default-browser-check", true),
		chromedp.Flag("disable-notifications", true),
		chromedp.Flag("disable-web-security", true),
		chromedp.Flag("disable-webgl", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("no-first-run", true),
		chromedp.Flag("proxy-server", proxyServer()),
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
	} else {
		allocOpts = append(allocOpts, chromedp.UserAgent(defaultUA))
	}
	dir, err := os.MkdirTemp(os.TempDir(), "chromedp-runner-*")
	if err == nil && dir != "" {
		defer os.RemoveAll(dir)
		allocOpts = append(allocOpts, chromedp.UserDataDir(dir))
	}
	ctx, cancel := chromedp.NewExecAllocator(ctx, allocOpts...)
	defer cancel()

	return screenshotStart[T](ctx, input, options...)
}

func screenshotStart[T As](ctx context.Context, input *url.URL, options ...ScreenshotOption) (shot *Screenshots[T], err error) {
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
	var img T
	var pdf T
	var har T
	var raw T
	var title string
	var dataLength int64

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
				var err error
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
			atomic.AddInt64(&dataLength, v.DataLength)
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

	captureAction := screenshotAction[T](&img, opts)
	exportHTML := exportHTML[T](&raw, opts)
	saveAsPDF := printPDF[T](&pdf, opts)
	if err := chromedp.Run(ctx, chromedp.Tasks{
		dom.Enable(),
		page.Enable(),
		network.Enable(),
		stealth(),
		setCookies(opts),
		setLocalStorage(input, opts),
		page.SetDownloadBehavior(page.SetDownloadBehaviorBehaviorDeny),
		navigateAndWaitFor(url, "networkAlmostIdle"),
		chromedp.Sleep(time.Second),
		evaluate(input),
		scrollToBottom(ctx),
		chromedp.Title(&title),
		captureAction,
		exportHTML,
		saveAsPDF,
		chromedp.ResetViewport(),
		chromedp.Sleep(time.Second),
		closePageAction(),
	}); err != nil && !errors.Is(err, chromedp.ErrPollingTimeout) {
		return shot, err
	}

	// Wait for all the go routines to complete
	wg.Wait()

	_ = compose[T](requestsID, nRequests, nResponses, opts, url, &har)
	shot = &Screenshots[T]{
		URL:   revertURI(url),
		PDF:   pdf,
		HAR:   har,
		HTML:  raw,
		Image: img,
		Title: title,

		DataLength: atomic.LoadInt64(&dataLength),
	}

	return shot, nil
}

// https://github.com/chromedp/chromedp/blob/875d6f4a3453149639d7fa83a2fa473b743fc33f/poll.go#L88-L127
func scrollToBottom(ctx context.Context) chromedp.Action {
	// This function script scrolls down 150px once. It returns a boolean for `chromedp.PollFunction`
	// to determine whether to execute next if the current height is less than the total height.
	// If an exception occurs, it will return true for termilate.
	//
	// Due to accuracy, the `currentHeight` may always be less than `the'scrollHeight`, add 1px
	// `currentHeight` to ensure scrolled to bottom.
	const script = `()=>{
    let distance = 150;
    let scrollHeight = 0;
    let currentHeight = 0;

    try {
        scrollHeight = document.documentElement.scrollHeight || document.body.scrollHeight;
        currentHeight = window.innerHeight + window.pageYOffset + 1;
    } catch (e) {
        return true;
    }

    window.scrollBy(0, distance);
    if (currentHeight >= scrollHeight) {
        return true;
    }
    return false;
}`

	timeout := 15 * time.Second
	deadline, ok := ctx.Deadline()
	if ok {
		timeout = deadline.Sub(time.Now())
		idle := 5 * time.Second
		if timeout > idle {
			timeout -= idle
		}
	}

	// Scroll down to the bottom line by line, which is controlled by `chromedp.WithPollingInterval`.
	return chromedp.Tasks{
		chromedp.PollFunction(script, nil, chromedp.WithPollingTimeout(timeout), chromedp.WithPollingInterval(150*time.Millisecond)),
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
func screenshotAction[T As](res *T, options ScreenshotOptions) chromedp.Action {
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

			buf, err := page.CaptureScreenshot().
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
			switch t := (interface{})(res).(type) {
			case *Byte:
				*t = buf
			case *Path:
				err = helper.WriteFile(options.Files.Image, buf, perm)
				if err == nil {
					*t = Path(options.Files.Image)
				}
			}
			return err
		}),
	}
}

func printPDF[T As](res *T, options ScreenshotOptions) chromedp.Action {
	if !options.PrintPDF {
		return chromedp.Tasks{}
	}

	return chromedp.Tasks{
		chromedp.ActionFunc(func(ctx context.Context) error {
			buf, _, err := page.PrintToPDF().WithLandscape(true).WithPrintBackground(true).Do(ctx)
			switch t := (interface{})(res).(type) {
			case *Byte:
				*t = buf
			case *Path:
				err = helper.WriteFile(options.Files.PDF, buf, perm)
				if err == nil {
					*t = Path(options.Files.PDF)
				}
			}
			return err
		}),
	}
}

func exportHTML[T As](res *T, options ScreenshotOptions) chromedp.Action {
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
			raw, err := dom.GetOuterHTML().WithNodeID(node.NodeID).Do(ctx)
			if err != nil {
				return err
			}
			buf := helper.String2Byte(raw)
			switch t := (interface{})(res).(type) {
			case *Byte:
				*t = buf
			case *Path:
				err = helper.WriteFile(options.Files.HTML, buf, perm)
				if err == nil {
					*t = Path(options.Files.HTML)
				}
			}
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

		// timeout := 30 * time.Second
		// ctx, cancel := context.WithTimeout(ctx, timeout)
		// defer cancel()

		return waitFor(ctx, eventName)
	}
}

// waitFor blocks until eventName is received.
// Examples of events you can wait for:
//
//	init, DOMContentLoaded, firstPaint,
//	firstContentfulPaint, firstImagePaint,
//	firstMeaningfulPaintCandidate,
//	load, networkAlmostIdle, firstMeaningfulPaint, networkIdle
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

	Files Files

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

type Files struct {
	Image string
	HTML  string
	PDF   string
	HAR   string
}

func AppendToFile(f Files) ScreenshotOption {
	return func(opts *ScreenshotOptions) {
		opts.Files = f
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
//
//	example.com:
//	  - name: 'foo'
//	    value: 'bar'
//	  - name: 'zoo'
//	    value: 'zoo'
//	example.org:
//	  - name: 'foo'
//	    value: 'bar'
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

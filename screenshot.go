package screenshot

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
	"github.com/wabarc/logger"
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
}

// Screenshoter is a webpage screenshot interface.
type Screenshoter interface {
	Screenshot(ctx context.Context, urls []string, options ...ScreenshotOption) ([]Screenshots, error)
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

func (s *chromeRemoteScreenshoter) Screenshot(ctx context.Context, urls []string, options ...ScreenshotOption) ([]Screenshots, error) {
	if s.url == "" {
		return nil, fmt.Errorf("Can't connect to headless browser")
	}

	ctx, _ = chromedp.NewRemoteAllocator(ctx, s.url)
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()

	return screenshotStart(ctx, urls, options...)
}

func Screenshot(ctx context.Context, urls []string, options ...ScreenshotOption) ([]Screenshots, error) {
	// https://github.com/chromedp/chromedp/blob/b56cd66f9cebd6a1fa1283847bbf507409d48225/allocate.go#L53
	var allocOpts = append(
		chromedp.DefaultExecAllocatorOptions[:],
		chromedp.CombinedOutput(log.Writer()),
		chromedp.NoDefaultBrowserCheck,
		chromedp.Flag("ignore-certificate-errors", true),
		chromedp.Flag("allow-running-insecure-content", true),
		chromedp.Flag("disable-web-security", true),
		chromedp.Flag("disable-webgl", true),
	)
	if noHeadless := os.Getenv("CHROMEDP_NO_HEADLESS"); noHeadless != "" && noHeadless != "false" {
		allocOpts = append(allocOpts, chromedp.Flag("headless", false))
	}
	if noSandbox := os.Getenv("CHROMEDP_NO_SANDBOX"); noSandbox != "" && noSandbox != "false" {
		allocOpts = append(allocOpts, chromedp.NoSandbox)
	}
	if disableGPU := os.Getenv("CHROMEDP_DISABLE_GPU"); disableGPU != "" && disableGPU != "false" {
		allocOpts = append(allocOpts, chromedp.DisableGPU)
	}
	ctx, _ = chromedp.NewExecAllocator(ctx, allocOpts...)
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()

	return screenshotStart(ctx, urls, options...)
}

func screenshotStart(ctx context.Context, urls []string, options ...ScreenshotOption) ([]Screenshots, error) {
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

	var wg sync.WaitGroup
	screenshots := make([]Screenshots, 0, len(urls))
	for _, url := range urls {
		wg.Add(1)
		url := convertURI(url)
		go func(url string) {
			var buf []byte
			var pdf []byte
			var raw string
			var title string
			// var ok bool

			chromedp.ListenTarget(ctx, func(ev interface{}) {
				switch ev := ev.(type) {
				case *page.EventJavascriptDialogOpening:
					go func() {
						if err := chromedp.Run(ctx,
							page.HandleJavaScriptDialog(true),
						); err != nil {
							log.Print(err)
						}
					}()
				// case *page.EventDocumentOpened:
				// 	return
				// case *network.EventRequestWillBeSent:
				// 	return
				// case *network.EventResponseReceived:
				// 	return
				case *network.EventLoadingFinished:
					logger.Debug("[screenshot] EventLoadingFinished: %v", ev.RequestID)
					return
				}
			})

			ctx, _ = chromedp.NewContext(ctx)
			captureAction := screenshotAction(url, &buf, opts)
			exportHTML := exportHTML(&raw, opts)
			saveAsPDF := printPDF(&pdf, opts)
			if err := chromedp.Run(ctx, chromedp.Tasks{
				page.Enable(),
				network.Enable(),
				// enableLifeCycleEvents(),
				page.SetDownloadBehavior(page.SetDownloadBehaviorBehaviorDeny),
				navigateAndWaitFor(url, "networkIdle"),
				// chromedp.Navigate(url),
				chromedp.WaitReady("body"),
				chromedp.Title(&title),
				evaluate(&buf),
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
			screenshots = append(screenshots, Screenshots{
				URL:   revertURI(url),
				PDF:   pdf,
				HTML:  []byte(raw),
				Image: buf,
				Title: title,
			})
			wg.Done()
		}(url)
	}
	wg.Wait()

	return screenshots, nil
}

func evaluate(res interface{}) chromedp.EvaluateAction {
	// Scroll down to the bottom line by line
	return chromedp.Tasks{
		chromedp.EvaluateAsDevTools(`
			var totalHeight = 0;
			var distance = 100;
			var timer = setInterval(() => {
				var scrollHeight = document.body.scrollHeight;
				window.scrollBy(0, distance);
				totalHeight += distance;
				if (totalHeight >= scrollHeight) {
					clearInterval(timer);
				}
			}, 100)
		`, res),
		chromedp.Sleep(15 * time.Second),
	}
}

// Note: this will override the viewport emulation settings.
func screenshotAction(url string, res *[]byte, options ScreenshotOptions) chromedp.Action {
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
			if contentSize == nil {
				return nil
			}

			// Limit dimensions
			if options.MaxHeight > 0 && contentSize.Height > float64(options.MaxHeight) {
				contentSize.Height = float64(options.MaxHeight)
			}

			*res, err = page.CaptureScreenshot().
				WithCaptureBeyondViewport(true).
				WithQuality(options.Quality).
				WithFormat(options.Format).
				WithClip(&page.Viewport{
					X:      contentSize.X,
					Y:      contentSize.Y,
					Width:  contentSize.Width,
					Height: contentSize.Height,
					Scale:  1,
				}).Do(ctx)
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
		chromedp.OuterHTML("html", res, chromedp.ByQuery),
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

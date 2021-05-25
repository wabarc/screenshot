package screenshot

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

type Screenshots struct {
	URL   string
	Title string
	Data  []byte
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

	allocCtx, cancel := chromedp.NewRemoteAllocator(ctx, s.url)
	defer cancel()

	return screenshotStart(allocCtx, urls, options...)
}

func Screenshot(ctx context.Context, urls []string, options ...ScreenshotOption) ([]Screenshots, error) {
	// https://github.com/chromedp/chromedp/blob/b56cd66f9cebd6a1fa1283847bbf507409d48225/allocate.go#L53
	var allocOpts = append(chromedp.DefaultExecAllocatorOptions[:], chromedp.Flag("ignore-certificate-errors", true))
	if noSandbox := os.Getenv("CHROMEDP_NO_SANDBOX"); noSandbox != "" && noSandbox != "false" {
		allocOpts = append(allocOpts, chromedp.NoSandbox)
	}
	if disableGPU := os.Getenv("CHROMEDP_DISABLE_GPU"); disableGPU != "" && disableGPU != "false" {
		allocOpts = append(allocOpts, chromedp.DisableGPU)
	}
	allocCtx, cancel := chromedp.NewExecAllocator(ctx, allocOpts...)
	defer cancel()

	return screenshotStart(allocCtx, urls, options...)
}

func screenshotStart(allocCtx context.Context, urls []string, options ...ScreenshotOption) ([]Screenshots, error) {
	var browserOpts []chromedp.ContextOption
	if debug := os.Getenv("CHROMEDP_DEBUG"); debug != "" && debug != "false" {
		browserOpts = append(browserOpts, chromedp.WithDebugf(log.Printf))
	}
	windowCtx, cancel := chromedp.NewContext(allocCtx, browserOpts...)
	defer cancel()

	var opts ScreenshotOptions
	for _, o := range options {
		o(&opts)
	}

	// run a no-op action to allocate the browser
	// if err := chromedp.Run(windowCtx, chromedp.ActionFunc(func(_ context.Context) error {
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
			var title string
			captureAction := screenshotAction(url, &buf, opts)
			tabCtx, _ := chromedp.NewContext(windowCtx)

			chromedp.ListenTarget(tabCtx, func(ev interface{}) {
				switch ev.(type) {
				case *page.EventJavascriptDialogOpening:
					go func() {
						if err := chromedp.Run(tabCtx,
							page.HandleJavaScriptDialog(false),
						); err != nil {
							log.Print(err)
						}
					}()
				case *page.EventDocumentOpened:
					return
				}
			})

			if err := chromedp.Run(tabCtx, chromedp.Tasks{
				// emulation.SetDeviceMetricsOverride(opts.Width, opts.Height, opts.ScaleFactor, opts.Mobile),
				chromedp.Navigate(url),
				chromedp.Sleep(2 * time.Second),
				chromedp.Title(&title),
				chromedp.WaitReady("body"),
				captureAction,
				// closePageAction(),
			}); err != nil {
				log.Print(err)
				buf = nil
			}
			screenshots = append(screenshots, Screenshots{
				URL:   url,
				Data:  buf,
				Title: title,
			})
			wg.Done()
		}(url)
	}
	wg.Wait()

	return screenshots, nil
}

// Note: this will override the viewport emulation settings.
func screenshotAction(url string, res *[]byte, options ScreenshotOptions) chromedp.Action {
	return chromedp.Tasks{
		enableLifeCycleEvents(),
		// chromedp.Navigate(url),
		navigateAndWaitFor(url, "networkIdle"),
		chromedp.WaitReady("body"),
		chromedp.ActionFunc(func(ctx context.Context) (err error) {
			if res == nil {
				return nil
			}
			_, exp, err := runtime.Evaluate(`window.scrollTo(0,document.body.scrollHeight);`).Do(ctx)
			if err != nil {
				return err
			}
			if exp != nil {
				return exp
			}

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

			width, height := int64(math.Ceil(contentSize.Width)), int64(math.Ceil(contentSize.Height))

			// force viewport emulation
			err = emulation.SetDeviceMetricsOverride(width, height, 1, false).
				WithScreenOrientation(&emulation.ScreenOrientation{
					Type:  emulation.OrientationTypePortraitPrimary,
					Angle: 0,
				}).Do(ctx)
			if err != nil {
				return err
			}

			// Limit dimensions
			if options.MaxHeight > 0 && contentSize.Height > float64(options.MaxHeight) {
				contentSize.Height = float64(options.MaxHeight)
			}

			*res, err = page.CaptureScreenshot().
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

func closePageAction() chromedp.Action {
	return chromedp.ActionFunc(func(ctx context.Context) (err error) {
		return page.Close().Do(ctx)
	})
}

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

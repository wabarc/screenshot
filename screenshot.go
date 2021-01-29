package screenshot

import (
	"context"
	"log"
	"math"
	"os"

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

func Screenshot(ctx context.Context, urls []string, options ...ScreenshotOption) ([]Screenshots, error) {
	var allocOpts = chromedp.DefaultExecAllocatorOptions[:]
	if noSandbox := os.Getenv("CHROMEDP_NO_SANDBOX"); noSandbox != "" && noSandbox != "false" {
		allocOpts = append(allocOpts, chromedp.NoSandbox)
	}
	if disableGPU := os.Getenv("CHROMEDP_DISABLE_GPU"); disableGPU != "" && disableGPU != "false" {
		allocOpts = append(allocOpts, chromedp.DisableGPU)
	}
	allocCtx, cancel := chromedp.NewExecAllocator(ctx, allocOpts...)
	defer cancel()

	var browserOpts []chromedp.ContextOption
	if debug := os.Getenv("CHROMEDP_DEBUG"); debug != "" && debug != "false" {
		browserOpts = append(browserOpts, chromedp.WithDebugf(log.Printf))
	}
	ctxt, cancel := chromedp.NewContext(allocCtx, browserOpts...)
	defer cancel()

	var opts ScreenshotOptions
	for _, o := range options {
		o(&opts)
	}

	screenshots := make([]Screenshots, 0, len(urls))
	for _, url := range urls {
		var buf []byte
		var title string
		captureAction := screenshotAction(url, opts.Quality, &buf, opts)

		if err := chromedp.Run(ctxt,
			// emulation.SetDeviceMetricsOverride(opts.Width, opts.Height, opts.ScaleFactor, opts.Mobile),
			chromedp.Navigate(url),
			chromedp.Title(&title),
			captureAction,
			// closePageAction(),
		); err != nil {
			buf = nil
		}
		screenshots = append(screenshots, Screenshots{
			URL:   url,
			Data:  buf,
			Title: title,
		})
	}

	return screenshots, nil
}

// Note: this will override the viewport emulation settings.
func screenshotAction(url string, quality int64, res *[]byte, options ScreenshotOptions) chromedp.Action {
	return chromedp.Tasks{
		chromedp.Navigate(url),
		chromedp.WaitReady("body"),
		chromedp.ActionFunc(func(ctx context.Context) (err error) {
			if res == nil {
				return
			}

			_, exp, err := runtime.Evaluate(`window.scrollTo(0,document.body.scrollHeight);`).Do(ctx)
			if err != nil {
				return err
			}
			if exp != nil {
				return exp
			}

			// get layout metrics
			_, _, contentSize, err := page.GetLayoutMetrics().Do(ctx)
			if err != nil {
				return err
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
			if options.MaxHeight >0 && contentSize.Height > float64(options.MaxHeight) {
				contentSize.Height = float64(options.MaxHeight)
			}
			// params.Format = page.CaptureScreenshotFormatJpeg
			*res, err = page.CaptureScreenshot().
				WithQuality(quality).
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

// ScreenshotOptions is the options used by Screenshot.
type ScreenshotOptions struct {
	Width  int64
	Height int64
	Mobile bool
	Format string // jpg, png, default: png.

	Quality int64
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
		opts.Format = format
	}
}

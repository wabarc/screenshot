package main

import (
	"context"
	"flag"
	"fmt"
	"io/ioutil"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gabriel-vasile/mimetype"
	"github.com/wabarc/helper"
	"github.com/wabarc/screenshot"
)

func main() {
	var timeout uint64
	var format string
	var remoteAddr string
	var img bool
	var pdf bool
	var raw bool
	var har bool

	flag.Uint64Var(&timeout, "timeout", 60, "Screenshot timeout.")
	flag.StringVar(&format, "format", "png", "Screenshot file format.")
	flag.StringVar(&remoteAddr, "remote-addr", "", "Headless browser remote address, such as 127.0.0.1:9222")
	flag.BoolVar(&img, "img", false, "Save as image")
	flag.BoolVar(&pdf, "pdf", false, "Save as PDF")
	flag.BoolVar(&raw, "raw", false, "Save as raw html")
	flag.BoolVar(&har, "har", false, "Export HAR")

	flag.Parse()
	if !img && !pdf && !raw {
		img = true
	}

	args := flag.Args()
	if len(args) < 1 {
		flag.Usage()
		e := os.Args[0]
		fmt.Printf("  %s url [url]\n\n", e)
		fmt.Printf("example:\n  %s https://example.org/ https://example.com/\n\n", e)
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
	defer cancel()

	var shot screenshot.Screenshots
	var opts = []screenshot.ScreenshotOption{
		screenshot.ScaleFactor(1),
		screenshot.PrintPDF(pdf), // print pdf
		screenshot.RawHTML(raw),  // export html
		screenshot.DumpHAR(har),  // export har
		screenshot.Quality(100),  // image quality
	}
	var wg sync.WaitGroup
	for k := range args {
		wg.Add(1)
		go func(link string) {
			defer wg.Done()
			input, err := url.Parse(link)
			if err != nil {
				fmt.Println(link, "=>", fmt.Sprintf("%v", err))
				return
			}
			if remoteAddr != "" {
				remote, er := screenshot.NewChromeRemoteScreenshoter(remoteAddr)
				if er != nil {
					fmt.Println(link, "=>", er.Error())
					return
				}
				shot, err = remote.Screenshot(ctx, input, opts...)
			} else {
				shot, err = screenshot.Screenshot(ctx, input, opts...)
			}
			if err != nil {
				if err == context.DeadlineExceeded {
					fmt.Println(link, "=>", err.Error())
					return
				}
				fmt.Println(link, "=>", err.Error())
				return
			}

			if shot.URL == "" || shot.Image == nil {
				return
			}
			writeFile(shot.URL, shot.Image)
			writeFile(shot.URL, shot.HTML)
			writeFile(shot.URL, shot.PDF)
			writeFile(shot.URL, shot.HAR)
		}(args[k])
	}
	wg.Wait()
}

func writeFile(uri string, data []byte) {
	if data == nil {
		return
	}

	mtype := mimetype.Detect(data)
	filename := helper.FileName(uri, mtype.String())
	// Replace json with har
	if strings.HasSuffix(filename, ".json") {
		filename = strings.TrimSuffix(filename, "json") + "har"
	}
	if err := ioutil.WriteFile(filename, data, 0o600); err != nil {
		fmt.Println(uri, "=>", err)
		return
	}
	fmt.Println(uri, "=>", filename)
}

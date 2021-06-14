package main

import (
	"context"
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"time"

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

	flag.Uint64Var(&timeout, "timeout", 60, "Screenshot timeout.")
	flag.StringVar(&format, "format", "png", "Screenshot file format.")
	flag.StringVar(&remoteAddr, "remote-addr", "", "Headless browser remote address, such as 127.0.0.1:9222")
	flag.BoolVar(&img, "img", false, "Save as image")
	flag.BoolVar(&pdf, "pdf", false, "Save as PDF")
	flag.BoolVar(&raw, "raw", false, "Save as raw html")

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

	var str string
	for _, arg := range args {
		str += fmt.Sprintf(" %s", arg)
	}

	urls := helper.MatchURL(str)

	var err error
	var shots []screenshot.Screenshots
	var opts = []screenshot.ScreenshotOption{
		screenshot.ScaleFactor(1),
		screenshot.PrintPDF(pdf), // print pdf
		screenshot.RawHTML(raw),  // export html
		screenshot.Quality(100),  // image quality
	}
	if remoteAddr != "" {
		remote, er := screenshot.NewChromeRemoteScreenshoter(remoteAddr)
		if er != nil {
			fmt.Println(er)
			return
		}
		shots, err = remote.Screenshot(ctx, urls, opts...)
	} else {
		shots, err = screenshot.Screenshot(ctx, urls, opts...)
	}
	if err != nil {
		if err == context.DeadlineExceeded {
			fmt.Println(err.Error())
			return
		}
		fmt.Println(err.Error())
		return
	}

	for _, shot := range shots {
		if shot.URL == "" || shot.Image == nil {
			continue
		}
		if img {
			if err := ioutil.WriteFile(helper.FileName(shot.URL, http.DetectContentType(shot.Image)), shot.Image, 0o644); err != nil {
				fmt.Println(err)
				continue
			}
		}
		if pdf {
			if err := ioutil.WriteFile(helper.FileName(shot.URL, http.DetectContentType(shot.PDF)), shot.PDF, 0o644); err != nil {
				fmt.Println(err)
				continue
			}
		}
		if raw {
			if err := ioutil.WriteFile(helper.FileName(shot.URL, http.DetectContentType(shot.HTML)), shot.HTML, 0o644); err != nil {
				fmt.Println(err)
				continue
			}
		}
	}
}

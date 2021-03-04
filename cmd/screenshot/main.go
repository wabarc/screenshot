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

	flag.Uint64Var(&timeout, "timeout", 60, "Screenshot timeout")
	flag.StringVar(&format, "format", "png", "Screenshot file format, default: png")

	flag.Parse()

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

	shots, err := screenshot.Screenshot(ctx, urls, screenshot.ScaleFactor(1), screenshot.Format(format))
	if err != nil {
		if err == context.DeadlineExceeded {
			fmt.Println(err.Error())
			return
		}
		fmt.Println(err.Error())
		return
	}

	for _, shot := range shots {
		if shot.URL == "" || shot.Data == nil {
			continue
		}
		if err := ioutil.WriteFile(helper.FileName(shot.URL, http.DetectContentType(shot.Data)), shot.Data, 0o644); err != nil {
			fmt.Println(err)
			continue
		}
	}
}

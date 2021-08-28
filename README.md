# screenshot

[![Go Report Card](https://goreportcard.com/badge/github.com/wabarc/screenshot)](https://goreportcard.com/report/github.com/wabarc/screenshot)
[![Go Reference](https://img.shields.io/badge/godoc-reference-blue.svg)](https://pkg.go.dev/github.com/wabarc/screenshot)
[![Releases](https://img.shields.io/github/v/release/wabarc/screenshot.svg?include_prereleases&color=blue)](https://github.com/wabarc/screenshot/releases)

Screenshot is a project that capture and save webpage as image using [chromedp](https://github.com/chromedp/chromedp).

This repository is a *work in progress*.

## Prerequisite

- Chrome/Chromium

## Installation

From source:

```sh
go get github.com/wabarc/screenshot
```

From [gobinaries.com](https://gobinaries.com):

```sh
curl -sf https://gobinaries.com/wabarc/screenshot | sh
```

From [releases](https://github.com/wabarc/screenshot/releases)

## Environments

- CHROMEDP_DEBUG
- CHROMEDP_NO_HEADLESS
- CHROMEDP_NO_SANDBOX
- CHROMEDP_DISABLE_GPU
- CHROMEDP_USER_AGENT

## Credits

- [chromedp](https://github.com/chromedp)

## License

This software is released under the terms of the GNU General Public License v3.0. See the [LICENSE](https://github.com/wabarc/screenshot/blob/main/LICENSE) file for details.

// Copyright 2022 Wayback Archiver. All rights reserved.
// Use of this source code is governed by the GNU GPL v3
// license that can be found in the LICENSE file.

package screenshot // import "github.com/wabarc/screenshot"

import (
	"fmt"
	"net/url"

	"github.com/chromedp/chromedp"
	"golang.org/x/net/publicsuffix"
)

var scripts = map[domain]string{
	"substack.com": `let maybeLater = document.querySelector('button.maybe-later'); if (maybeLater !== null) { maybeLater.click(); }`,

	"archiveofourown.org": `let tosAgree = document.querySelector('#tos_agree');
if (tosAgree !== null) {
    tosAgree.click();
}
let tosAccept = document.querySelector('#accept_tos');
if (tosAccept !== null) {
    tosAccept.click();
}`,

	"douban.com": `let noteMask = document.querySelector('.ui-overlay-mask');
let noteDial = document.querySelector('iframe[src*="login"]');
if (noteDial !== null && noteMask !== null) {
  if (noteMask.style.display === '') {
    document.elementFromPoint(1, 1).click();
  }
}
let noteReadMore = document.querySelector('#link-report .taboola-open');
if (noteReadMore !== null) {
  noteReadMore.click();
}`,
}

type domain string

func evaluate(u *url.URL) chromedp.Action {
	dom, err := publicsuffix.EffectiveTLDPlusOne(u.Hostname())
	if err != nil {
		return chromedp.Tasks{}
	}

	script := scripts[domain(dom)]
	if script == "" {
		return chromedp.Tasks{}
	}

	script = fmt.Sprintf("() => {\n try{ \n%s\n }catch(_){};\n return true;\n}\n", script)
	// action := chromedp.PollFunction(script, nil, chromedp.WithPollingTimeout(5*time.Second))
	action := chromedp.Evaluate(script, nil, chromedp.EvalIgnoreExceptions)

	return chromedp.Tasks{action}
}

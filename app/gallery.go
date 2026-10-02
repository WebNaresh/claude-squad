package app

import (
	"claude-squad/config"
	"claude-squad/session"
	"fmt"
	"html"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// The image gallery: every file a Claude session sent, on one web page in
// Chrome (newest batch first, before/after side by side, with Claude's
// captions), scrolled to the one clicked. One page to scroll through beats
// opening the images one by one in Preview.

var galleryNameRe = regexp.MustCompile(`[^A-Za-z0-9_-]+`)

var imageExts = map[string]bool{".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true, ".svg": true}

// openGallery writes the session's gallery page and opens it, scrolled to
// focus (a file path; "" = the newest batch).
func openGallery(e *session.ExternalSession, focus string) tea.Cmd {
	return func() tea.Msg {
		batches, err := e.SentBatches()
		if err != nil {
			return err
		}
		if len(batches) == 0 {
			return fmt.Errorf("this session hasn't sent any files that still exist")
		}
		dir, err := config.GetConfigDir()
		if err != nil {
			return err
		}
		dir = filepath.Join(dir, "gallery")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		page := filepath.Join(dir, galleryNameRe.ReplaceAllString(e.Name, "-")+".html")
		if err := os.WriteFile(page, []byte(galleryHTML(e.Title(), batches, focus)), 0o644); err != nil {
			return err
		}
		// Chrome when it's installed, else the default browser.
		if exec.Command("open", "-a", "Google Chrome", "--", page).Run() != nil {
			if out, err := exec.Command("open", "--", page).CombinedOutput(); err != nil {
				return fmt.Errorf("could not open the gallery: %s", strings.TrimSpace(string(out)))
			}
		}
		n := 0
		for _, b := range batches {
			n += len(b.Files)
		}
		logEvent("gallery opened for %s: %d file(s)", e.Name, n)
		return fmt.Errorf("opened %d image(s) in the browser", n)
	}
}

func galleryHTML(title string, batches []session.SentBatch, focus string) string {
	if focus == "" {
		focus = batches[len(batches)-1].Files[0]
	}
	var b strings.Builder
	b.WriteString(`<!doctype html><meta charset="utf-8"><title>` + html.EscapeString(title) + ` · images</title>
<style>
body{margin:0;background:#1e2128;color:#e6e6e6;font:14px -apple-system,system-ui,sans-serif}
header{position:sticky;top:0;background:#1e2128ee;padding:12px 20px;border-bottom:1px solid #333;font-weight:600}
section{padding:16px 20px;border-bottom:1px solid #2c2f36}
.cap{color:#aab;margin:0 0 10px}
.grid{display:grid;grid-template-columns:repeat(auto-fill,minmax(520px,1fr));gap:14px}
figure{margin:0;background:#262a32;border:2px solid transparent;border-radius:8px;padding:8px}
figure.focus{border-color:#7c6cf0}
img{width:100%;height:auto;border-radius:4px;display:block;cursor:zoom-in}
figcaption{color:#99a;font-size:12px;margin-top:6px;word-break:break-all}
a.file{color:#9ab4ff}
</style>
<header>` + html.EscapeString(title) + ` · images Claude sent (newest first) · click an image for full size</header>
`)
	for i := len(batches) - 1; i >= 0; i-- {
		bt := batches[i]
		b.WriteString("<section>")
		if bt.Caption != "" {
			b.WriteString(`<p class="cap">` + html.EscapeString(bt.Caption) + "</p>")
		}
		b.WriteString(`<div class="grid">`)
		for _, f := range bt.Files {
			u := (&url.URL{Scheme: "file", Path: f}).String()
			cls := ""
			if f == focus {
				cls = ` class="focus" id="focus"`
			}
			name := html.EscapeString(filepath.Base(f))
			if imageExts[strings.ToLower(filepath.Ext(f))] {
				fmt.Fprintf(&b, `<figure%s><a href="%s" target="_blank"><img src="%s"></a><figcaption>%s</figcaption></figure>`, cls, u, u, name)
			} else {
				fmt.Fprintf(&b, `<figure%s><a class="file" href="%s" target="_blank">%s</a></figure>`, cls, u, name)
			}
		}
		b.WriteString("</div></section>\n")
	}
	b.WriteString(`<script>addEventListener("load",function(){var f=document.getElementById("focus");if(f)f.scrollIntoView({block:"center"})});</script>`)
	return b.String()
}

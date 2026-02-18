package main

import (
	"fmt"
	"html"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/afero"
)

var (
	disTag = map[bool]string{
		true:  "",
		false: "DISABLED",
	}
	charset = map[bool]string{
		true:  "UTF-8",
		false: "ISO-8859-1",
	}
	padding = map[bool]string{
		true:  "2px",
		false: "0px",
	}
)

func htErr(w http.ResponseWriter, msg string, err error) {
	w.Header().Set("Content-Type", "text/plain")
	w.Header().Set("Cache-Control", *cacheCtl)
	fmt.Fprintln(w, msg, ":", err)
	log.Printf("error: %v : %v", msg, err)
}

func joinPath(base, path string) string {
	base = strings.TrimSuffix(base, "/")
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return base + path
}

func header(w http.ResponseWriter, uDir, sort, extraCSS string, modern bool) {
	eDir := html.EscapeString(uDir)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", *cacheCtl)
	w.Write([]byte(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="description" content="` + html.EscapeString(*siteDesc) + `">
<link rel="icon" type="image/x-icon" href="` + joinPath(wfmPfx, "/favicon.ico") + `">
<link rel="stylesheet" href="` + joinPath(wfmPfx, "/static/style.css") + `">
<link rel="stylesheet" href="` + joinPath(wfmPfx, "/static/fontawesome/css/all.min.css") + `">
<title>` + html.EscapeString(*siteName) + ` : ` + eDir + `</title>
`))
	if extraCSS != "" {
		w.Write([]byte(`<style>` + extraCSS + `</style>
`))
	}
	w.Write([]byte(`</head>
<body>
<div class="drop-overlay" id="dropOverlay"><span>Drop file to upload</span></div>
<form action="` + joinPath(wfmPfx, "/") + `" method="POST" enctype="multipart/form-data" id="wfmForm">
<input type="hidden" name="dir" value="` + eDir + `">
<input type="hidden" name="sort" value="` + sort + `">
`))
}

func footer(w http.ResponseWriter) {
	w.Write([]byte(`
<script src="` + joinPath(wfmPfx, "/static/app.js") + `"></script>
</form>
</body>
</html>
`))
}

// writeUploadRejectedPage renders an HTML error page when an upload is rejected (e.g. disallowed file type).
func writeUploadRejectedPage(w http.ResponseWriter, uDir, eSort, filename string, modern bool) {
	header(w, uDir, eSort, "", modern)
	backURL := joinPath(wfmPfx, "/") + "?dir=" + url.PathEscape(uDir)
	if eSort != "" {
		backURL += "&sort=" + eSort
	}
	w.Write([]byte(`
<div class="upload-error">
    <p class="upload-error-title">Upload rejected</p>
    <p>The file <strong>` + html.EscapeString(filename) + `</strong> was not uploaded.</p>
    <p>Only text, PDF, and image files are allowed.</p>
    <p><a href="` + html.EscapeString(backURL) + `" class="btn btn-primary">Back to directory</a></p>
</div>
`))
	footer(w)
}

func redirect(w http.ResponseWriter, uUrl string) {
	w.Header().Set("Location", uUrl)
	w.Header().Set("Content-Type", "text/html")
	w.Header().Set("Cache-Control", *cacheCtl)
	w.WriteHeader(302)

	u := html.EscapeString(uUrl)
	w.Write([]byte(`<HTML>
	<HEAD>
	<META HTTP-EQUIV="refresh" CONTENT="0; URL=` + u + `">
	</HEAD>
	<BODY>
	If you see this, your browser did not redirect. <A HREF="` + u + `">Click here...</A>
    </BODY>
	</HTML>
    `))
}

func upDnDir(uDir, uBn string, wfs afero.Fs) string {
	o := strings.Builder{}
	o.WriteString("<OPTION VALUE=\"/\">/ - Root</OPTION>\n")
	p := "/"
	i := 0
	for _, n := range strings.Split(uDir, string(os.PathSeparator))[1:] {
		p = p + n + "/"
		opt := ""
		if p == uDir+"/" {
			opt = "DISABLED"
		}
		i++
		o.WriteString("<OPTION " + opt + " VALUE=\"" +
			html.EscapeString(filepath.Clean(p+"/"+uBn)) + "\">" +
			emit("&nbsp;&nbsp;", i) + " L " +
			html.EscapeString(n) + "</OPTION>\n")
	}
	d, err := afero.ReadDir(wfs, uDir)
	if err != nil {
		return o.String()
	}
	for _, n := range d {
		if !n.IsDir() || strings.HasPrefix(n.Name(), ".") {
			continue
		}
		o.WriteString("<OPTION VALUE=\"" +
			html.EscapeString(uDir+"/"+n.Name()+"/"+uBn) + "\">" +
			emit("&nbsp;&nbsp;&nbsp;", i) + " L " +
			html.EscapeString(n.Name()) + "</OPTION>\n")
	}
	return o.String()
}

package main

import (
	"fmt"
	"html"
	"log"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"

	"github.com/dustin/go-humanize"
	"github.com/spf13/afero"
)

var (
	rorw = map[bool]string{
		true:  "rw",
		false: "ro",
	}
)

func (r *wfmRequest) listFiles(hi string) {
	i := icons(r.modern)
	d, err := afero.ReadDir(r.fs, r.uDir)
	if err != nil {
		htErr(r.w, "Unable to read directory", err)
		return
	}
	sl := []string{}
	sortFiles(d, &sl, r.eSort)

	header(r.w, r.uDir, r.eSort, "", r.modern)
	toolbars(r.w, r.uDir, r.userName, sl, i, r.rwAccess)
	qeDir := strings.ReplaceAll(url.PathEscape(r.uDir), `%2F`, `/`)

	z := 0
	var total uint64
	var totItems int

	// List Directories First
	for _, f := range d {
		var ldir bool
		var li string
		if f.Mode()&os.ModeSymlink == os.ModeSymlink {
			ls, err := r.fs.Stat(r.uDir + "/" + f.Name())
			if err != nil {
				continue
			}
			ldir = ls.IsDir()
			li = i["li"]
		}
		if !f.IsDir() && !ldir {
			continue
		}
		if !*showDot && f.Name()[0:1] == "." {
			continue
		}
		z++
		qeFile := url.PathEscape(f.Name())
		heFile := html.EscapeString(f.Name())
		nUrl, err := url.JoinPath(wfmPfx, qeDir, qeFile)
		if err != nil {
			log.Printf("Unable to parse url: %v", err)
		}
		if r.eSort != "" {
			nUrl += `?sort=` + r.eSort
		}
		highlight := ""
		if f.Name() == hi {
			highlight = " highlight"
		}
		r.w.Write([]byte(`
        <li class="file-item dir` + highlight + `">
            <div class="file-name">
                <input type="checkbox" name="mulf" value="` + heFile + `" id="cb-dir-` + heFile + `">
                <a href="` + nUrl + `">` + i["di"] + heFile + `/</a>` + li + `
            </div>
            <div class="file-size">&nbsp;</div>
            <div class="file-actions">
		`))
		if r.rwAccess {
			r.w.Write([]byte(`
                <a href="` + wfmPfx + `?fn=renp&amp;dir=` + qeDir + `&amp;file=` + qeFile + `&amp;sort=` + r.eSort + `" class="action-link" title="Rename">` + i["re"] + `</a>
                <a href="` + wfmPfx + `?fn=movp&amp;dir=` + qeDir + `&amp;file=` + qeFile + `&amp;sort=` + r.eSort + `" class="action-link" title="Move">` + i["mv"] + `</a>
                <a href="` + wfmPfx + `?fn=delp&amp;dir=` + qeDir + `&amp;file=` + qeFile + `&amp;sort=` + r.eSort + `" class="action-link action-delete" title="Delete">` + i["rm"] + `</a>
		`))
		}
		r.w.Write([]byte(`
            </div>
        </li>
        `))
		totItems++
	}

	// List Files
	for _, f := range d {
		var ldir bool
		var li string
		if f.Mode()&os.ModeSymlink == os.ModeSymlink {
			ls, err := r.fs.Stat(r.uDir + "/" + f.Name())
			if err != nil {
				continue
			}
			ldir = ls.IsDir()
			li = i["li"]
		}
		if f.IsDir() || ldir {
			continue
		}
		if !*showDot && f.Name()[0:1] == "." {
			continue
		}
		z++
		qeFile := url.PathEscape(f.Name())
		heFile := html.EscapeString(f.Name())
		nUrl, err := url.JoinPath(wfmPfx, qeDir, qeFile)
		if err != nil {
			log.Printf("Unable to parse url: %v", err)
		}
		highlight := ""
		if f.Name() == hi {
			highlight = " highlight"
		}
		r.w.Write([]byte(`
        <li class="file-item` + highlight + `">
            <div class="file-name">
                <input type="checkbox" name="mulf" value="` + heFile + `" id="cb-file-` + heFile + `">
                <a href="` + nUrl + `">` + fileIcon(qeFile, r.modern) + ` ` + heFile + `</a>` + li + `
            </div>
            <div class="file-size">` + humanize.Bytes(uint64(f.Size())) + `</div>
            <div class="file-actions">
                <a href="` + wfmPfx + `?fn=down&amp;dir=` + qeDir + `&amp;file=` + qeFile + `" class="action-link" title="Download">` + i["dn"] + `</a>
		`))
		if r.rwAccess {
			r.w.Write([]byte(`
                <a href="` + wfmPfx + `?fn=edit&amp;dir=` + qeDir + `&amp;file=` + qeFile + `&amp;sort=` + r.eSort + `" class="action-link" title="Edit">` + i["ed"] + `</a>
                <a href="` + wfmPfx + `?fn=renp&amp;dir=` + qeDir + `&amp;file=` + qeFile + `&amp;sort=` + r.eSort + `" class="action-link" title="Rename">` + i["re"] + `</a>
                <a href="` + wfmPfx + `?fn=movp&amp;dir=` + qeDir + `&amp;file=` + qeFile + `&amp;sort=` + r.eSort + `" class="action-link" title="Move">` + i["mv"] + `</a>
                <a href="` + wfmPfx + `?fn=delp&amp;dir=` + qeDir + `&amp;file=` + qeFile + `&amp;sort=` + r.eSort + `" class="action-link action-delete" title="Delete">` + i["rm"] + `</a>
		`))
		}
		r.w.Write([]byte(`
            </div>
        </li>
        `))
		total = total + uint64(f.Size())
		totItems++
	}

	// Footer
	r.w.Write([]byte(`
    </ul>
    <div class="file-footer">` + fmt.Sprint(totItems) + ` items, ` + humanize.Bytes(total) + ` total</div>
</div>
`))
	footer(r.w)
}

func toolbars(w http.ResponseWriter, uDir, user string, sl []string, i map[string]string, rw bool) {
	eDir := html.EscapeString(uDir)
	qeDir := url.PathEscape(uDir)
	// Header
	w.Write([]byte(`
<header class="header">
    <div class="header-title">` + html.EscapeString(*siteName) + ` : ` + eDir + `</div>
    <div class="header-actions">
        <span>` + i[rorw[rw]] + `</span>
        <a href="` + wfmPfx + `?fn=logout">` + i["tid"] + html.EscapeString(user) + `</a>
        <a href="` + wfmPfx + `?fn=about&amp;dir=` + qeDir + `&amp;sort=">` + i["tve"] + ` v` + vers + `</a>
    </div>
</header>
`))

	// Toolbar
	w.Write([]byte(`
<div class="toolbar">
    <button type="submit" name="up" class="btn">` + i["tup"] + `<span class="btn-text">Up</span></button>
    <button type="submit" name="home" class="btn">` + i["tho"] + `<span class="btn-text">Home</span></button>
    <button type="submit" name="mdelp" class="btn btn-danger" ` + disTag[rw] + `>` + i["trm"] + `<span class="btn-text">Delete</span></button>
    <button type="submit" name="mmovp" class="btn" ` + disTag[rw] + `>` + i["tmv"] + `<span class="btn-text">Move</span></button>
    <button type="submit" name="mkd" class="btn" ` + disTag[rw] + `>` + i["tdi"] + `<span class="btn-text">New Folder</span></button>
    <button type="submit" name="mkf" class="btn" ` + disTag[rw] + `>` + i["tfi"] + `<span class="btn-text">New File</span></button>
    <input type="file" name="filename" class="btn btn-file" accept="*">
    <button type="submit" name="upload" class="btn btn-primary" ` + disTag[rw] + `>` + i["tul"] + `<span class="btn-text">Upload</span></button>
</div>
`))

	// File List Container (Name, Size, Actions - no time column)
	w.Write([]byte(`
<div class="file-list-container">
    <div class="file-list-header">
        <div><a href="` + wfmPfx + `/` + qeDir + `?sort=` + sl[0] + `">` + html.EscapeString(sl[1]) + `</a></div>
        <div><a href="` + wfmPfx + `/` + qeDir + `?sort=` + sl[2] + `">` + html.EscapeString(sl[3]) + `</a></div>
        <div>Actions</div>
    </div>
    <ul class="file-list">
`))

}

func sortFiles(f []os.FileInfo, l *[]string, by string) {
	switch by {
	// size
	case "sa":
		sort.Slice(f, func(i, j int) bool {
			return f[i].Size() < f[j].Size()
		})
		*l = []string{"na", "Name", "sd", "Size"}
		return
	case "sd":
		sort.Slice(f, func(i, j int) bool {
			return f[i].Size() > f[j].Size()
		})
		*l = []string{"na", "Name", "sa", "Size"}
		return

	// time (kept for sort order, not shown in UI)
	case "ta":
		sort.Slice(f, func(i, j int) bool {
			return f[i].ModTime().Before(f[j].ModTime())
		})
		*l = []string{"na", "Name", "sa", "Size"}
		return
	case "td":
		sort.Slice(f, func(i, j int) bool {
			return f[i].ModTime().After(f[j].ModTime())
		})
		*l = []string{"na", "Name", "sa", "Size"}
		return

	// name
	case "nd":
		sort.Slice(f, func(i, j int) bool {
			return f[i].Name() > f[j].Name()
		})
		*l = []string{"na", "Name", "sa", "Size"}
		return
	default:
		*l = []string{"nd", "Name", "sa", "Size"}
		return
	}
}

// fa returns a Font Awesome icon (safe to embed in HTML).
func fa(classes string) string {
	return `<i class="fa-solid ` + classes + ` fa-fw"></i>`
}

func icons(m bool) map[string]string {
	if m {
		return map[string]string{
			"fi": fa("fa-file") + " ",
			"di": fa("fa-folder") + " ",
			"li": " " + fa("fa-link") + " ",

			"rm": fa("fa-trash"),
			"mv": fa("fa-arrow-right-arrow-left"),
			"re": fa("fa-i-cursor"),
			"ed": fa("fa-pen"),
			"dn": fa("fa-download"),

			"tcd": fa("fa-utensils") + " ",
			"tup": fa("fa-arrow-up") + " ",
			"tho": fa("fa-house") + " ",
			"tre": fa("fa-arrows-rotate") + " ",
			"trm": fa("fa-trash") + " ",
			"tmv": fa("fa-arrow-right-arrow-left") + " ",
			"tfi": fa("fa-file") + " ",
			"tdi": fa("fa-folder-plus") + " ",
			"tul": fa("fa-upload") + " ",

			"tid": fa("fa-user") + " ",
			"tve": fa("fa-circle-info") + " ",

			"rw": fa("fa-lock-open") + " rw",
			"ro": fa("fa-lock") + " ro",
		}
	}

	return map[string]string{
		"fi": " ",
		"di": " ",
		"li": " (link)",
		"rm": "[rm]",
		"mv": "[mv]",
		"re": "[re]",
		"ed": "[ed]",
		"dn": "[dn]",
		"tup": "^ ",
		"tho": "~ ",
		"tre": "&reg; ",
		"tid": "User: ",
		"tve": "WFM ",
		"rw": "[rw]",
		"ro": "[ro]",
	}
}

func fileIcon(f string, m bool) string {
	if !m {
		return ""
	}
	s := strings.Split(f, ".")
	ext := ""
	if len(s) > 1 {
		ext = strings.ToLower(s[len(s)-1])
	}
	switch ext {
	case "iso", "udf":
		return fa("fa-compact-disc") + " "
	case "mp4", "mov", "qt", "avi", "mpg", "mpeg", "mkv":
		return fa("fa-file-video") + " "
	case "gif", "png", "jpg", "jpeg", "ico", "webp", "bmp", "tif", "tiff", "heif", "heic":
		return fa("fa-file-image") + " "
	case "deb", "rpm", "dpkg", "apk", "msi", "pkg":
		return fa("fa-box") + " "
	case "zip", "rar", "7z", "z", "gz", "bz2", "xz", "lz", "tgz", "tbz", "txz", "arj", "lha", "tar":
		return fa("fa-file-zipper") + " "
	case "imd", "img", "raw", "dd", "tap", "dsk", "dmg":
		return fa("fa-hard-drive") + " "
	case "txt", "log", "csv", "md", "mhtml", "html", "htm", "cfg", "conf", "ini", "json", "xml":
		return fa("fa-file-lines") + " "
	case "pdf", "ps", "doc", "docx", "xls", "xlsx", "rtf":
		return fa("fa-file-pdf") + " "
	case "url", "desktop", "webloc":
		return fa("fa-globe") + " "
	}
	return fa("fa-file") + " "
}

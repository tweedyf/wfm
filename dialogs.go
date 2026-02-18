package main

import (
	"bytes"
	"fmt"
	"html"
	"runtime"
	"strings"

	"github.com/dustin/go-humanize"
	"github.com/spf13/afero"
)

func selOpt(s string, f ...struct{ v, n string }) string {
	var o []string
	var m = make(map[string]string)
	m[s] = "selected"
	m[""] = "disabled"
	for _, i := range f {
		o = append(o, fmt.Sprintf("<option value=\"%v\" %v>%v</option>", html.EscapeString(i.v), m[i.v], html.EscapeString(i.n)))
	}
	return strings.Join(o, "\n")
}

func (r *wfmRequest) prompt(action string, mul []string) {
	header(r.w, r.uDir, r.eSort, "", r.modern)

	actionTitle := action
	switch action {
	case "mkdir":
		actionTitle = "Create Directory"
	case "mkfile":
		actionTitle = "Create File"
	case "mkurl":
		actionTitle = "Create URL File"
	case "rename":
		actionTitle = "Rename"
	case "move":
		actionTitle = "Move"
	case "delete":
		actionTitle = "Delete"
	case "multi_delete":
		actionTitle = "Delete Multiple Files"
	case "multi_move":
		actionTitle = "Move Multiple Files"
	}

	r.w.Write([]byte(`
<div class="modal-overlay">
    <div class="modal">
        <div class="modal-header">` + html.EscapeString(actionTitle) + `</div>
        <div class="modal-body">
    `))

	switch action {
	case "mkdir":
		r.w.Write([]byte(`
            <div class="form-group">
                <label>Enter name for the new directory:</label>
                <input type="text" name="file" value="" required autofocus>
            </div>
        `))
	case "mkfile":
		r.w.Write([]byte(`
            <div class="form-group">
                <label>Enter name for the new file:</label>
                <input type="text" name="file" value="" required autofocus>
            </div>
        `))
	case "mkurl":
		r.w.Write([]byte(`
            <div class="form-group">
                <label>Enter name for the new URL file:</label>
                <input type="text" name="file" value="" required autofocus>
            </div>
            <div class="form-group">
                <label>Destination URL:</label>
                <input type="text" name="url" value="" required>
            </div>
        `))
	case "rename":
		eBn := html.EscapeString(r.uFbn)
		r.w.Write([]byte(`
            <div class="form-group">
                <label>Enter new name for the file <strong>` + eBn + `</strong>:</label>
                <input type="text" name="dst" value="` + eBn + `" required autofocus>
                <input type="hidden" name="file" value="` + eBn + `">
            </div>
        `))
	case "move":
		eBn := html.EscapeString(r.uFbn)
		r.w.Write([]byte(`
            <div class="form-group">
                <label>Select destination folder for <strong>` + eBn + `</strong>:</label>
                <select name="dst" required autofocus>
                ` + upDnDir(r.uDir, "", r.fs) + `
                </select>
                <input type="hidden" name="file" value="` + eBn + `">
            </div>
        `))
	case "delete":
		var a string
		fi, _ := r.fs.Stat(r.uDir + "/" + r.uFbn)
		if fi != nil {
			if fi.IsDir() {
				a = "directory - recursively"
			} else {
				a = "file, size " + humanize.Bytes(uint64(fi.Size()))
			}
		}
		eBn := html.EscapeString(r.uFbn)
		r.w.Write([]byte(`
            <div class="form-group">
                <p>Are you sure you want to delete:</p>
                <p><strong>` + eBn + `</strong> (` + a + `)</p>
                <input type="hidden" name="file" value="` + eBn + `">
            </div>
        `))
	case "multi_delete":
		fmt.Fprintf(r.w, `<div class="form-group">
            <p>Are you sure you want to delete from <strong>%v</strong>:</p>
            <ul style="list-style: square; margin-left: 1.5rem; margin-top: 0.5rem;">
        `, html.EscapeString(r.uDir))
		for _, f := range mul {
			fE := html.EscapeString(f)
			fmt.Fprintf(r.w, `<input type="hidden" name="mulf" value="%s">
                <li>%v</li>
        `, fE, fE)
		}
		fmt.Fprintln(r.w, `</ul></div>`)
	case "multi_move":
		fmt.Fprintf(r.w, `<div class="form-group">
            <p>Move from: <strong>%v</strong></p>
            <label>To:</label>
            <select name="dst" required autofocus>%v</select>
            <p style="margin-top: 1rem;">Items:</p>
            <ul style="list-style: square; margin-left: 1.5rem; margin-top: 0.5rem;">
        `,
			html.EscapeString(r.uDir),
			upDnDir(r.uDir, r.uFbn, r.fs),
		)
		for _, f := range mul {
			fE := html.EscapeString(f)
			fmt.Fprintf(r.w, `<input type="hidden" name="mulf" value="%s">
                <li>%v</li>
        `, fE, fE)
		}
		fmt.Fprintln(r.w, `</ul></div>`)
	}

	r.w.Write([]byte(`
        </div>
        <div class="modal-footer">
            <button type="submit" name="OK" class="btn btn-primary" ` + disTag[r.rwAccess] + `>OK</button>
            <button type="submit" name="cancel" class="btn">Cancel</button>
            <input type="hidden" name="fn" value="` + action + `">
        </div>
    </div>
</div>
    `))

	footer(r.w)
}

func (r *wfmRequest) editText() {
	fi, err := r.fs.Stat(r.uDir + "/" + r.uFbn)
	if err != nil {
		htErr(r.w, "Unable to get file attributes", err)
		return
	}
	if fi.Size() > 1<<20 {
		htErr(r.w, "edit", fmt.Errorf("the file is too large for editing"))
		return
	}
	f, err := afero.ReadFile(r.fs, r.uDir+"/"+r.uFbn)
	if err != nil {
		htErr(r.w, "Unable to read file", err)
		return
	}
	le := *defLe
	if bytes.IndexByte(f, '\r') != -1 {
		le = "CRLF"
	}
	header(r.w, r.uDir, r.eSort, ``, r.modern)
	r.w.Write([]byte(`
<div class="editor-container">
    <div class="editor-header">
        <div>File Editor: ` + html.EscapeString(r.uFbn) + `</div>
        <div>
            <label>Line Endings:</label>
            <select name="crlf">
            ` + selOpt(le, []struct{ v, n string }{
		{"LF", "LF (Unix)"},
		{"CRLF", "CRLF (Windows)"},
	}...) + `
            </select>
        </div>
    </div>
    <div class="editor-content">
        <pre id="wfmEditor" contenteditable="true">` + html.EscapeString(string(f)) + `</pre>
        <textarea name="text" id="wfmEditorInput" style="display:none;">` + html.EscapeString(string(f)) + `</textarea>
    </div>
    <div class="editor-header" style="justify-content: flex-end; gap: 0.5rem;">
        <button type="submit" name="save" class="btn btn-primary" ` + disTag[r.rwAccess] + `>Save</button>
        <button type="submit" name="cancel" class="btn">Cancel</button>
        <input type="hidden" name="dir" value="` + html.EscapeString(r.uDir) + `">
        <input type="hidden" name="file" value="` + html.EscapeString(r.uFbn) + `">
    </div>
</div>
    `))
	footer(r.w)
}

func (r *wfmRequest) about(ua string) {
	header(r.w, r.uDir, r.eSort, "", r.modern)

	r.w.Write([]byte(`
<div class="modal-overlay">
    <div class="modal">
        <div class="modal-header">Web File Manager</div>
        <div class="modal-body">
            <p><strong>WFM Version v` + vers + `</strong></p>
            <p><a href="https://github.com/tenox7/wfm/">https://github.com/tenox7/wfm/</a></p>
            <p>Written by Antoni Sawicki Et Al.</p>
            <p>Copyright &copy; 1994-2025 by Antoni Sawicki</p>
    `))

	if *aboutRnt {
		fmt.Fprintf(r.w, `
            <p style="margin-top: 1rem; padding-top: 1rem; border-top: 1px solid var(--border);">
                Build: %v %v-%v<br>
                Agent: %v
            </p>
        `,
			runtime.Version(),
			runtime.GOARCH,
			runtime.GOOS,
			ua)
	}

	r.w.Write([]byte(`
        </div>
        <div class="modal-footer">
            <button type="submit" name="OK" class="btn btn-primary">OK</button>
        </div>
    </div>
</div>
    `))

	footer(r.w)
}

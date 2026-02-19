package main

import (
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/spf13/afero"
)

type wfmRequest struct {
	fs       afero.Fs
	w        http.ResponseWriter
	userName string
	remAddr  string
	rwAccess bool
	modern   bool
	eSort    string // escaped sort order
	uDir     string // unescaped directory name
	uFbn     string // unescaped file base name
}

func wfmMain(w http.ResponseWriter, r *http.Request) {
	r.ParseMultipartForm(*formMaxMem)

	// Handle login form POST before auth
	if r.FormValue("fn") == "login" && r.Method == http.MethodPost && r.FormValue("login") == "1" {
		handleLoginPOST(w, r)
		return
	}
	// Handle logout: clear session and redirect to login
	if r.FormValue("fn") == "logout" {
		logout(w, r)
		return
	}

	uName, uAccess := auth(w, r)
	if uName == "" {
		return
	}
	log.Printf("req from=%q user=%q uri=%q form=%v agent=%v", r.RemoteAddr, uName, r.RequestURI, noText(r.Form), r.UserAgent())

	if *dumpHeader {
		dump, err := httputil.DumpRequest(r, false)
		if err == nil {
			log.Printf("debug: %v", string(dump))
		}
	}

	fs := wfmFs
	if uName != "" && uName != "n/a" && uName != "admin" {
		fs = afero.NewBasePathFs(fs, "/"+uName)
	}

	wfm := &wfmRequest{
		userName: uName,
		rwAccess: uAccess,
		remAddr:  r.RemoteAddr,
		w:        w,
		eSort:    r.FormValue("sort"),
		modern: func() bool {
			return strings.HasPrefix(r.UserAgent(), "Mozilla/5") && r.Header.Get("Accept-Charset") == ""
		}(),
		fs:   fs,
		uFbn: filepath.Base(unescapeOrEmpty(r.FormValue("file"))),
		uDir: filepath.Clean(unescapeOrEmpty(r.FormValue("dir"))),
	}

	// directory can come either from form value or URI Path
	if wfm.uDir == "" || wfm.uDir == "." {
		// TODO(tenox): use url.Parse() instead
		u, err := url.PathUnescape(r.URL.Path)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		wfm.uDir = filepath.Clean("/" + strings.TrimPrefix(u, wfmPfx))
	}
	if wfm.uDir == "" || wfm.uDir == "." {
		wfm.uDir = "/"
	}

	// button clicked
	switch {
	case r.FormValue("mkd") != "":
		wfm.prompt("mkdir", nil)
		return
	case r.FormValue("mkf") != "":
		wfm.prompt("mkfile", nil)
		return
	case r.FormValue("mkb") != "":
		wfm.prompt("mkurl", nil)
		return
	case r.FormValue("mdelp") != "":
		wfm.prompt("multi_delete", r.Form["mulf"])
		return
	case r.FormValue("mmovp") != "":
		wfm.prompt("multi_move", r.Form["mulf"])
		return
	case r.FormValue("upload") != "":
		f, h, err := r.FormFile("filename")
		if err != nil {
			htErr(w, "upload", err)
			return
		}
		wfm.uploadFile(h, f)
		return
	case r.FormValue("save") != "":
		wfm.saveText(r.FormValue("text"))
		return
	case r.FormValue("up") != "":
		up, err := url.JoinPath(wfmPfx, filepath.Dir(wfm.uDir))
		if err != nil {
			htErr(w, "up path build", err)
			return
		}
		if wfm.eSort != "" {
			up += "?sort=" + wfm.eSort
		}
		redirect(w, up)
		return
	case r.FormValue("home") != "":
		wfm.uDir = "/"
		wfm.listFiles(filepath.Base(r.FormValue("hi")))
		return
	case r.FormValue("cancel") != "" || r.FormValue("fn") == "cancel":
		wfm.listFiles(filepath.Base(r.FormValue("hi")))
		return
	}

	// form action submitted
	fn := r.FormValue("fn")
	modalCancel := r.FormValue("cancel") != "" || r.FormValue("modal_confirm") == "0"
	if modalCancel && (fn == "mkdir" || fn == "mkfile" || fn == "mkurl" || fn == "rename" || fn == "move" || fn == "delete" || fn == "multi_delete" || fn == "multi_move") {
		wfm.listFiles("")
		return
	}
	switch fn {
	case "disp":
		wfm.dispFile()
	case "down":
		wfm.downFile()
	case "edit":
		wfm.editText()
	case "mkdir":
		wfm.mkdir()
	case "mkfile":
		wfm.mkfile()
	case "mkurl":
		wfm.mkurl(r.FormValue("url"))
	case "rename":
		wfm.renFile(r.FormValue("dst"))
	case "renp":
		wfm.prompt("rename", nil)
	case "movp":
		wfm.prompt("move", nil)
	case "delp":
		wfm.prompt("delete", nil)
	case "move":
		wfm.moveFiles([]string{wfm.uFbn}, r.FormValue("dst"))
	case "delete":
		wfm.deleteFiles([]string{wfm.uFbn})
	case "multi_delete":
		wfm.deleteFiles(r.Form["mulf"])
	case "multi_move":
		wfm.moveFiles(r.Form["mulf"], r.FormValue("dst"))
	case "logout":
		logout(w, r)
	case "chpass":
		handleChpass(w, r, wfm)
	case "about":
		wfm.about(r.UserAgent())
	default:
		wfm.dispOrDir(filepath.Base(r.FormValue("hi")))
	}
}

func handleChpass(w http.ResponseWriter, r *http.Request, wfm *wfmRequest) {
	newPass := r.FormValue("new_pass")
	confirmPass := r.FormValue("confirm_pass")
	if newPass != confirmPass {
		redirectWithChpassError(w, wfm, "New passwords do not match")
		return
	}
	if len(newPass) == 0 {
		redirectWithChpassError(w, wfm, "New password cannot be empty")
		return
	}
	err := updateUserPassword(wfm.userName, r.FormValue("current_pass"), newPass)
	if err != nil {
		redirectWithChpassError(w, wfm, err.Error())
		return
	}
	redirect(w, redirectURL(wfm))
}

func redirectURL(wfm *wfmRequest) string {
	u := wfmPfx + "?dir=" + url.PathEscape(wfm.uDir)
	if wfm.eSort != "" {
		u += "&sort=" + wfm.eSort
	}
	return u
}

func redirectWithChpassError(w http.ResponseWriter, wfm *wfmRequest, msg string) {
	u := redirectURL(wfm) + "&chpass_error=" + url.QueryEscape(msg)
	redirect(w, u)
}

func unescapeOrEmpty(s string) string {
	u, err := url.QueryUnescape(s)
	if err != nil {
		log.Printf("unescape: %q err=%v", s, err)
		return ""
	}
	return u
}

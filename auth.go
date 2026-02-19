package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"html"
	"log"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const sessionCookieName = "wfm_session"

func getSessionSecret() []byte {
	s := *sessionSecret
	if s == "" {
		s = "wfm-insecure-default-change-me"
		log.Printf("auth: using default session_secret; set -session_secret for production")
	}
	return []byte(s)
}

func setSessionCookie(w http.ResponseWriter, user string, rw bool) {
	exp := time.Now().Unix() + int64(*sessionMaxAge)
	payload := fmt.Sprintf("%s|%v|%d", user, rw, exp)
	b64 := base64.URLEncoding.EncodeToString([]byte(payload))
	mac := hmac.New(sha256.New, getSessionSecret())
	mac.Write([]byte(b64))
	sig := hex.EncodeToString(mac.Sum(nil))
	cookieVal := b64 + "." + sig
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    cookieVal,
		Path:     wfmPfx,
		MaxAge:   *sessionMaxAge,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   false,
	})
}

func getSessionCookie(r *http.Request) (user string, rw bool, ok bool) {
	c, err := r.Cookie(sessionCookieName)
	if err != nil || c.Value == "" {
		return "", false, false
	}
	parts := strings.SplitN(c.Value, ".", 2)
	if len(parts) != 2 {
		return "", false, false
	}
	b64, sig := parts[0], parts[1]
	mac := hmac.New(sha256.New, getSessionSecret())
	mac.Write([]byte(b64))
	if subtle.ConstantTimeCompare([]byte(hex.EncodeToString(mac.Sum(nil))), []byte(sig)) != 1 {
		return "", false, false
	}
	dec, err := base64.URLEncoding.DecodeString(b64)
	if err != nil {
		return "", false, false
	}
	fields := strings.Split(string(dec), "|")
	if len(fields) != 3 {
		return "", false, false
	}
	exp, err := strconv.ParseInt(fields[2], 10, 64)
	if err != nil || time.Now().Unix() > exp {
		return "", false, false
	}
	rw = fields[1] == "true"
	return fields[0], rw, true
}

func clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     wfmPfx,
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

// verifyLogin checks username and password against the htpasswd file. Returns (rw, true) on success, (false, false) on failure.
// Reloads the htpasswd and ACL files before checking so external changes (e.g. htpasswd CLI) are visible without restart.
func verifyLogin(username, password string) (rw bool, ok bool) {
	if htpasswdFile == nil {
		return false, false
	}
	reloadHtpasswd()
	reloadACL()
	if !htpasswdFile.Match(username, password) {
		return false, false
	}
	return userRW(username), true
}

func loginURL() string {
	return wfmPfx + "?fn=login"
}

func serveLoginPage(w http.ResponseWriter, r *http.Request, errorMsg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", *cacheCtl)
	action := wfmPfx
	errHTML := ""
	if errorMsg != "" {
		errHTML = `<p class="login-error">` + html.EscapeString(errorMsg) + `</p>`
	}
	title := html.EscapeString(*siteName) + " – Login"
	w.Write([]byte(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>` + title + `</title>
<link rel="icon" type="image/x-icon" href="` + html.EscapeString(joinPath(wfmPfx, "/favicon.ico")) + `">
<link rel="stylesheet" href="` + html.EscapeString(joinPath(wfmPfx, "/static/style.css")) + `">
<link rel="stylesheet" href="` + html.EscapeString(joinPath(wfmPfx, "/static/fontawesome/css/all.min.css")) + `">
</head>
<body>
<div class="login-page">
  <div class="login-box">
    <h1 class="login-title">` + html.EscapeString(*siteName) + `</h1>
    <p class="login-subtitle">Sign in</p>
    ` + errHTML + `
    <form method="POST" action="` + html.EscapeString(action) + `" class="login-form">
      <input type="hidden" name="fn" value="login">
      <input type="hidden" name="redirect" value="` + html.EscapeString(r.FormValue("redirect")) + `">
      <label for="wfm_user">Username</label>
      <input type="text" id="wfm_user" name="wfm_user" autocomplete="username" required autofocus>
      <label for="wfm_pass">Password</label>
      <input type="password" id="wfm_pass" name="wfm_pass" autocomplete="current-password" required>
      <button type="submit" name="login" value="1" class="btn btn-primary btn-block">Log in</button>
    </form>
  </div>
  <div class="login-box">
    Notice: the file manager uses cookies for logging in and out.
  </div>
</div>
</body>
</html>
`))
}

func auth(w http.ResponseWriter, r *http.Request) (string, bool) {
	if htpasswdFile == nil {
		return "n/a", *noPwdDbRW
	}

	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		log.Print(err)
		return "", false
	}

	if f2b.check(ip) {
		log.Printf("auth: %v is banned", ip)
		http.Error(w, "Too many bad username/password attempts", http.StatusTooManyRequests)
		return "", false
	}

	if user, rw, ok := getSessionCookie(r); ok {
		return user, rw
	}

	if r.FormValue("fn") == "login" && r.Method == http.MethodGet {
		serveLoginPage(w, r, "")
		return "", false
	}

	// No valid session: redirect to login (preserve path for after login if desired)
	redir := loginURL()
	if r.URL.RawQuery != "" && r.FormValue("fn") != "" {
		redir = loginURL() + "&redirect=" + url.QueryEscape(r.URL.Path+"?"+r.URL.RawQuery)
	} else if r.URL.Path != "" && strings.TrimSuffix(r.URL.Path, "/") != strings.TrimSuffix(wfmPfx, "/") {
		redir = loginURL() + "&redirect=" + url.QueryEscape(r.URL.Path)
	}
	http.Redirect(w, r, redir, http.StatusFound)
	return "", false
}

func logout(w http.ResponseWriter, r *http.Request) {
	clearSessionCookie(w)
	http.Redirect(w, r, loginURL(), http.StatusFound)
}

func handleLoginPOST(w http.ResponseWriter, r *http.Request) {
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		ip = r.RemoteAddr
	}
	if f2b.check(ip) {
		http.Error(w, "Too many attempts", http.StatusTooManyRequests)
		return
	}
	user := strings.TrimSpace(r.FormValue("wfm_user"))
	pass := r.FormValue("wfm_pass")
	if user == "" || pass == "" {
		f2b.ban(ip)
		serveLoginPage(w, r, "Username and password required")
		return
	}
	rw, ok := verifyLogin(user, pass)
	if !ok {
		log.Printf("auth: login failed ip=%v user=%q", ip, user)
		f2b.ban(ip)
		serveLoginPage(w, r, "Invalid username or password")
		return
	}
	go f2b.unban(ip)
	setSessionCookie(w, user, rw)
	redirectTo := wfmPfx
	if r.FormValue("redirect") != "" {
		if path, err := url.QueryUnescape(r.FormValue("redirect")); err == nil && path != "" {
			redirectTo = path
		}
	}
	http.Redirect(w, r, redirectTo, http.StatusFound)
}

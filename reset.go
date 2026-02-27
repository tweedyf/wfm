package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"html"
	"log"
	"math/big"
	"net/http"
	"net/smtp"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// In-memory password reset token store.
// Tokens are short-lived and cleared on process restart.

type resetTokenEntry struct {
	Username string
	Expires  time.Time
}

type resetTokenStore struct {
	mu     sync.Mutex
	tokens map[string]resetTokenEntry
}

var resetTokens = newResetTokenStore()

func newResetTokenStore() *resetTokenStore {
	return &resetTokenStore{
		tokens: make(map[string]resetTokenEntry),
	}
}

func (s *resetTokenStore) create(username string, ttl time.Duration) (string, error) {
	token, err := randomToken(32)
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tokens[token] = resetTokenEntry{
		Username: username,
		Expires:  time.Now().Add(ttl),
	}
	return token, nil
}

func (s *resetTokenStore) get(token string) (resetTokenEntry, bool) {
	if token == "" {
		return resetTokenEntry{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.tokens[token]
	if !ok {
		return resetTokenEntry{}, false
	}
	if time.Now().After(entry.Expires) {
		delete(s.tokens, token)
		return resetTokenEntry{}, false
	}
	return entry, true
}

func (s *resetTokenStore) consume(token string) (resetTokenEntry, bool) {
	if token == "" {
		return resetTokenEntry{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.tokens[token]
	if !ok {
		return resetTokenEntry{}, false
	}
	if time.Now().After(entry.Expires) {
		delete(s.tokens, token)
		return resetTokenEntry{}, false
	}
	delete(s.tokens, token)
	return entry, true
}

// Simple local captcha used for the password reset form.

type captchaEntry struct {
	Answer  int
	Expires time.Time
}

type captchaStore struct {
	mu   sync.Mutex
	data map[string]captchaEntry
}

var resetCaptcha = newCaptchaStore()

func newCaptchaStore() *captchaStore {
	return &captchaStore{
		data: make(map[string]captchaEntry),
	}
}

func (c *captchaStore) newChallenge() (id, question string) {
	// small addition question like "3 + 7 = ?"
	a := randomInt(1, 9)
	b := randomInt(1, 9)
	answer := a + b
	id, _ = randomToken(16)
	if id == "" {
		id = fmt.Sprintf("%d-%d-%d", time.Now().UnixNano(), a, b)
	}

	c.mu.Lock()
	c.data[id] = captchaEntry{
		Answer:  answer,
		Expires: time.Now().Add(15 * time.Minute),
	}
	c.mu.Unlock()

	question = fmt.Sprintf("%d + %d", a, b)
	return id, question
}

func (c *captchaStore) verify(id, answerStr string) bool {
	if id == "" || strings.TrimSpace(answerStr) == "" {
		return false
	}
	n, err := strconv.Atoi(strings.TrimSpace(answerStr))
	if err != nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.data[id]
	if !ok {
		return false
	}
	if time.Now().After(entry.Expires) {
		delete(c.data, id)
		return false
	}
	delete(c.data, id)
	return n == entry.Answer
}

// Helpers

func randomToken(nBytes int) (string, error) {
	buf := make([]byte, nBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func randomInt(min, max int) int {
	if max <= min {
		return min
	}
	diff := max - min + 1
	nBig, err := rand.Int(rand.Reader, big.NewInt(int64(diff)))
	if err != nil {
		return min
	}
	return min + int(nBig.Int64())
}

func randomPassword(length int) string {
	const letters = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	var b strings.Builder
	for i := 0; i < length; i++ {
		n := randomInt(0, len(letters)-1)
		b.WriteByte(letters[n])
	}
	return b.String()
}

// HTTP handlers for password reset flow.

// serveForgotPage renders the "forgot password" page with email + captcha.
func serveForgotPage(w http.ResponseWriter, r *http.Request, errorMsg string) {
	if *passwdDb == "" || getPasswdEmailPath() == "" {
		http.Error(w, "Password reset is not configured on this server.", http.StatusServiceUnavailable)
		return
	}

	id, question := resetCaptcha.newChallenge()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", *cacheCtl)

	action := wfmPfx
	errHTML := ""
	if errorMsg != "" {
		errHTML = `<p class="login-error">` + html.EscapeString(errorMsg) + `</p>`
	}
	title := html.EscapeString(*siteName) + " – Reset password"
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
    <h2 class="login-subtitle">Reset your password</h2>
    ` + errHTML + `
    <form method="POST" action="` + html.EscapeString(action) + `" class="login-form">
      <input type="hidden" name="fn" value="forgot">
      <label for="wfm_email">Email address</label>
      <input type="email" id="wfm_email" name="wfm_email" autocomplete="email" required autofocus>
      <label for="captcha_answer">Captcha: ` + html.EscapeString(question) + ` = ?</label>
      <input type="text" id="captcha_answer" name="captcha_answer" required inputmode="numeric">
      <input type="hidden" name="captcha_id" value="` + html.EscapeString(id) + `">
      <button type="submit" name="forgot" value="1" class="btn btn-primary btn-block">Send reset link</button>
    </form>
    <p style="margin-top: 2em;">If an account exists for the provided email, a reset link will be sent.</p>
    <p style="margin-top: 2em;"><a href="` + html.EscapeString(loginURL()) + `">Back to login</a></p>
  </div>
</div>
</body>
</html>
`))
}

// serveForgotSentPage renders a generic confirmation page after submitting the form.
func serveForgotSentPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", *cacheCtl)
	title := html.EscapeString(*siteName) + " – Reset link sent"
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
    <p class="login-subtitle">Check your email</p>
    <p>If an account exists for the provided email address, a password reset link has been sent.</p>
    <p><a href="` + html.EscapeString(loginURL()) + `" class="btn btn-primary btn-block">Back to login</a></p>
  </div>
</div>
</body>
</html>
`))
}

func handleForgotPOST(w http.ResponseWriter, r *http.Request) {
	if *passwdDb == "" || getPasswdEmailPath() == "" {
		http.Error(w, "Password reset is not configured on this server.", http.StatusServiceUnavailable)
		return
	}

	email := strings.TrimSpace(r.FormValue("wfm_email"))
	if email == "" {
		serveForgotPage(w, r, "Email address is required")
		return
	}
	if !resetCaptcha.verify(r.FormValue("captcha_id"), r.FormValue("captcha_answer")) {
		serveForgotPage(w, r, "Invalid captcha answer")
		return
	}

	// Reload email mappings on each request so external changes are visible.
	reloadEmails()

	username, ok := lookupUserByEmail(email)
	if ok {
		token, err := resetTokens.create(username, time.Hour)
		if err != nil {
			log.Printf("password reset: unable to create token for %q: %v", username, err)
		} else {
			if err := sendPasswordResetEmail(email, username, token); err != nil {
				log.Printf("password reset: send email to %q failed: %v", email, err)
			} else {
				log.Printf("password reset: token issued for user=%q email=%q", username, email)
			}
		}
	}

	// Always show generic confirmation, regardless of whether the email exists.
	serveForgotSentPage(w, r)
}

// serveResetInvalid renders an error when token is invalid or expired.
func serveResetInvalid(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", *cacheCtl)
	title := html.EscapeString(*siteName) + " – Invalid reset link"
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
    <p class="login-subtitle">Password reset link is invalid or has expired.</p>
    <p><a href="` + html.EscapeString(wfmPfx+"?fn=forgot") + `" class="btn btn-primary btn-block">Request a new reset link</a></p>
  </div>
</div>
</body>
</html>
`))
}

// serveResetPage shows the form to set a new password, prefilled with a suggested strong password.
func serveResetPage(w http.ResponseWriter, r *http.Request, errorMsg string) {
	token := r.FormValue("token")
	if token == "" {
		token = r.URL.Query().Get("token")
	}
	entry, ok := resetTokens.get(token)
	if !ok {
		serveResetInvalid(w, r)
		return
	}

	suggested := randomPassword(16)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", *cacheCtl)

	errHTML := ""
	if errorMsg != "" {
		errHTML = `<p class="login-error">` + html.EscapeString(errorMsg) + `</p>`
	}
	title := html.EscapeString(*siteName) + " – Choose new password"
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
    <p class="login-subtitle">Set a new password for your account</p>
    ` + errHTML + `
    <form method="POST" action="` + html.EscapeString(wfmPfx) + `" class="login-form">
      <input type="hidden" name="fn" value="reset">
      <input type="hidden" name="token" value="` + html.EscapeString(token) + `">
      <label for="new_pass">New password</label>
      <input type="password" id="new_pass" name="new_pass" autocomplete="new-password" required minlength="8" value="` + html.EscapeString(suggested) + `">
      <label for="confirm_pass">Confirm new password</label>
      <input type="password" id="confirm_pass" name="confirm_pass" autocomplete="new-password" required minlength="8" value="` + html.EscapeString(suggested) + `">
      <button type="submit" name="reset" value="1" class="btn btn-primary btn-block">Update password</button>
    </form>
  </div>
  <div class="login-box">
    <p>A strong, random password has been suggested. You can keep it or choose your own strong password.</p>
  </div>
</div>
</body>
</html>
`))
	_ = entry // entry is not currently displayed, but kept for potential future use.
}

func handleResetPOST(w http.ResponseWriter, r *http.Request) {
	token := r.FormValue("token")
	if token == "" {
		serveResetInvalid(w, r)
		return
	}

	entry, ok := resetTokens.get(token)
	if !ok {
		serveResetInvalid(w, r)
		return
	}

	newPass := r.FormValue("new_pass")
	confirmPass := r.FormValue("confirm_pass")
	if newPass == "" || confirmPass == "" {
		serveResetPage(w, r, "Password cannot be empty")
		return
	}
	if newPass != confirmPass {
		serveResetPage(w, r, "Passwords do not match")
		return
	}
	if len(newPass) < 8 {
		serveResetPage(w, r, "Password must be at least 8 characters long")
		return
	}

	// Consume token only after validation passes.
	entry, ok = resetTokens.consume(token)
	if !ok {
		serveResetInvalid(w, r)
		return
	}

	if err := resetUserPassword(entry.Username, newPass); err != nil {
		log.Printf("password reset: failed to update password for user=%q: %v", entry.Username, err)
		serveResetPage(w, r, "Unable to update password; contact the administrator")
		return
	}

	// Log the user in with the new password immediately.
	setSessionCookie(w, entry.Username, userRW(entry.Username))
	redirect(w, wfmPfx)
}

// sendPasswordResetEmail sends a reset link using the configured SMTP server.
func sendPasswordResetEmail(email, username, token string) error {
	if *smtpServer == "" || *smtpFrom == "" {
		return fmt.Errorf("smtp_server or smtp_from not configured")
	}
	resetURL := wfmPfx + "?fn=reset&token=" + url.QueryEscape(token)
	subject := fmt.Sprintf("[%s] Password reset", *siteName)
	body := fmt.Sprintf("Hello %s,\n\nA password reset was requested for your account on %s.\n\nTo choose a new password, open this link in your browser:\n\n%s\n\nIf you did not request this, you can ignore this message.\n", username, *siteName, resetURL)
	msg := fmt.Sprintf("To: %s\r\nFrom: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n%s", email, *smtpFrom, subject, body)

	return sendMail(*smtpServer, *smtpFrom, email, []byte(msg))
}

// sendMail is a small wrapper to send an email via the local SMTP server.
func sendMail(server, from, to string, msg []byte) error {
	return smtp.SendMail(server, nil, from, []string{to}, msg)
}

// Email confirmation for changing primary/secondary address.

type emailConfirmEntry struct {
	Username string
	Slot     string // "primary" or "secondary"
	NewEmail string
	Expires  time.Time
}

type emailConfirmStore struct {
	mu     sync.Mutex
	tokens map[string]emailConfirmEntry
}

var emailConfirmTokens = &emailConfirmStore{tokens: make(map[string]emailConfirmEntry)}

func (s *emailConfirmStore) create(username, slot, newEmail string, ttl time.Duration) (string, error) {
	token, err := randomToken(32)
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tokens[token] = emailConfirmEntry{
		Username: username,
		Slot:     slot,
		NewEmail: strings.TrimSpace(strings.ToLower(newEmail)),
		Expires:  time.Now().Add(ttl),
	}
	return token, nil
}

func (s *emailConfirmStore) consume(token string) (emailConfirmEntry, bool) {
	if token == "" {
		return emailConfirmEntry{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.tokens[token]
	if !ok {
		return emailConfirmEntry{}, false
	}
	if time.Now().After(entry.Expires) {
		delete(s.tokens, token)
		return emailConfirmEntry{}, false
	}
	delete(s.tokens, token)
	return entry, true
}

func sendEmailConfirmation(toEmail, username, slot, token string) error {
	if *smtpServer == "" || *smtpFrom == "" {
		return fmt.Errorf("smtp_server or smtp_from not configured")
	}
	confirmURL := wfmPfx + "?fn=confirm_email&token=" + url.QueryEscape(token)
	subject := fmt.Sprintf("[%s] Confirm email address change", *siteName)
	body := fmt.Sprintf("Hello %s,\n\nYou requested to set this address as your %s email for %s.\n\nTo confirm, open this link in your browser:\n\n%s\n\nIf you did not request this, you can ignore this message.\n", username, slot, *siteName, confirmURL)
	msg := fmt.Sprintf("To: %s\r\nFrom: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n%s", toEmail, *smtpFrom, subject, body)
	return sendMail(*smtpServer, *smtpFrom, toEmail, []byte(msg))
}

// serveEmailSettingsPage renders the page for managing primary/secondary email (requires auth).
func serveEmailSettingsPage(w http.ResponseWriter, r *http.Request, username, uDir, eSort, errorMsg, successMsg string) {
	primary, secondary := getEmailsForUser(username)
	backURL := wfmPfx
	if uDir != "" {
		backURL += "?dir=" + url.QueryEscape(uDir)
		if eSort != "" {
			backURL += "&sort=" + eSort
		}
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", *cacheCtl)
	errHTML := ""
	if errorMsg != "" {
		errHTML = `<p class="login-error">` + html.EscapeString(errorMsg) + `</p>`
	}
	successHTML := ""
	if successMsg != "" {
		successHTML = `<p class="login-success">` + html.EscapeString(successMsg) + `</p>`
	}
	title := html.EscapeString(*siteName) + " – Email addresses"
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
    <h2 class="login-subtitle">Email addresses</h2>
    ` + errHTML + successHTML + `
    <div class="form-group">
      <label>Primary email</label>
      <p><code>` + html.EscapeString(primary) + `</code></p>
      <form method="POST" action="` + html.EscapeString(wfmPfx) + `" class="login-form" style="display:inline-block;">
        <input type="hidden" name="fn" value="update_email">
        <input type="hidden" name="slot" value="primary">
        <input type="hidden" name="dir" value="` + html.EscapeString(uDir) + `">
        <input type="hidden" name="sort" value="` + html.EscapeString(eSort) + `">
        <input type="email" name="new_email" placeholder="New primary email" required>
        <button type="submit" class="btn btn-primary">Send confirmation to new address</button>
      </form>
    </div>
    <div class="form-group">
      <label>Secondary email</label>
      <p><code>` + html.EscapeString(secondary) + `</code></p>
      <form method="POST" action="` + html.EscapeString(wfmPfx) + `" class="login-form" style="display:inline-block;">
        <input type="hidden" name="fn" value="update_email">
        <input type="hidden" name="slot" value="secondary">
        <input type="hidden" name="dir" value="` + html.EscapeString(uDir) + `">
        <input type="hidden" name="sort" value="` + html.EscapeString(eSort) + `">
        <input type="email" name="new_email" placeholder="New secondary email">
        <button type="submit" class="btn btn-primary">Send confirmation to new address</button>
      </form>
    </div>
    <p><a href="` + html.EscapeString(backURL) + `" class="btn">Back</a></p>
  </div>
</div>
</body>
</html>
`))
}

// handleConfirmEmail consumes the token and updates the user's email file.
func handleConfirmEmail(w http.ResponseWriter, r *http.Request) {
	token := r.FormValue("token")
	if token == "" {
		token = r.URL.Query().Get("token")
	}
	entry, ok := emailConfirmTokens.consume(token)
	if !ok {
		serveConfirmEmailInvalid(w, r)
		return
	}
	if err := updateUserEmail(entry.Username, entry.Slot, entry.NewEmail); err != nil {
		log.Printf("email confirm: update failed for user=%q: %v", entry.Username, err)
		serveConfirmEmailInvalid(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", *cacheCtl)
	title := html.EscapeString(*siteName) + " – Email confirmed"
	w.Write([]byte(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>` + title + `</title>
<link rel="icon" type="image/x-icon" href="` + html.EscapeString(joinPath(wfmPfx, "/favicon.ico")) + `">
<link rel="stylesheet" href="` + html.EscapeString(joinPath(wfmPfx, "/static/style.css")) + `">
</head>
<body>
<div class="login-page">
  <div class="login-box">
    <h1 class="login-title">` + html.EscapeString(*siteName) + `</h1>
    <p class="login-subtitle">Email address updated</p>
    <p>Your ` + html.EscapeString(entry.Slot) + ` email has been updated. You can now use it for password reset.</p>
    <p><a href="` + html.EscapeString(loginURL()) + `" class="btn btn-primary">Log in</a></p>
  </div>
</div>
</body>
</html>
`))
}

// handleUpdateEmail handles POST from the email settings form: send confirmation to the new address.
func handleUpdateEmail(w http.ResponseWriter, r *http.Request, wfm *wfmRequest) {
	if getPasswdEmailPath() == "" {
		http.Error(w, "Email settings are not configured.", http.StatusServiceUnavailable)
		return
	}
	uDir, eSort := wfm.uDir, wfm.eSort
	username := wfm.userName
	slot := strings.TrimSpace(strings.ToLower(r.FormValue("slot")))
	if slot != "primary" && slot != "secondary" {
		redirectToEmailSettings(w, r, uDir, eSort, "Invalid slot", "")
		return
	}
	newEmail := strings.TrimSpace(r.FormValue("new_email"))
	if slot == "primary" && newEmail == "" {
		redirectToEmailSettings(w, r, uDir, eSort, "Primary email cannot be empty", "")
		return
	}
	// For secondary, empty newEmail means "clear secondary" (no confirmation email).
	if newEmail == "" && slot == "secondary" {
		if err := updateUserEmail(username, "secondary", ""); err != nil {
			redirectToEmailSettings(w, r, uDir, eSort, err.Error(), "")
			return
		}
		redirectToEmailSettings(w, r, uDir, eSort, "", "Secondary email cleared.")
		return
	}
	token, err := emailConfirmTokens.create(username, slot, newEmail, 24*time.Hour)
	if err != nil {
		log.Printf("email confirm: create token failed: %v", err)
		redirectToEmailSettings(w, r, uDir, eSort, "Unable to create confirmation; try again.", "")
		return
	}
	if err := sendEmailConfirmation(newEmail, username, slot, token); err != nil {
		log.Printf("email confirm: send to %q failed: %v", newEmail, err)
		redirectToEmailSettings(w, r, uDir, eSort, "Failed to send confirmation email; check server logs.", "")
		return
	}
	redirectToEmailSettings(w, r, uDir, eSort, "", "Confirmation email sent to "+newEmail+". Check your inbox and click the link to confirm.")
}

func redirectToEmailSettings(w http.ResponseWriter, r *http.Request, uDir, eSort, errMsg, successMsg string) {
	u := wfmPfx + "?fn=email_settings&dir=" + url.QueryEscape(uDir) + "&sort=" + url.QueryEscape(eSort)
	if errMsg != "" {
		u += "&email_error=" + url.QueryEscape(errMsg)
	}
	if successMsg != "" {
		u += "&email_success=" + url.QueryEscape(successMsg)
	}
	http.Redirect(w, r, u, http.StatusFound)
}

func serveConfirmEmailInvalid(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", *cacheCtl)
	title := html.EscapeString(*siteName) + " – Invalid or expired link"
	w.Write([]byte(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>` + title + `</title>
<link rel="stylesheet" href="` + html.EscapeString(joinPath(wfmPfx, "/static/style.css")) + `">
</head>
<body>
<div class="login-page">
  <div class="login-box">
    <h1 class="login-title">` + html.EscapeString(*siteName) + `</h1>
    <p class="login-subtitle">This confirmation link is invalid or has expired.</p>
    <p><a href="` + html.EscapeString(wfmPfx+"?fn=forgot") + `" class="btn btn-primary">Request password reset</a></p>
  </div>
</div>
</body>
</html>
`))
}

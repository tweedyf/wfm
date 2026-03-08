package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/tg123/go-htpasswd"
	"golang.org/x/crypto/bcrypt"
)

var (
	htpasswdFile *htpasswd.File
	userACL      map[string]bool // username -> rw (true) or ro (false). If nil, default rw for all.
	emailToUser  map[string]string // lowercased email -> username
)

func loadUsers() {
	f, err := htpasswd.New(*passwdDb, htpasswd.DefaultSystems, func(err error) {
		log.Printf("htpasswd: %v", err)
	})
	if err != nil {
		log.Fatal("unable to load password file: ", err)
	}
	htpasswdFile = f
	userACL = nil
	if *passwdAcl != "" {
		userACL = make(map[string]bool)
		file, err := os.Open(*passwdAcl)
		if err != nil {
			log.Fatal("unable to open passwd_acl file: ", err)
		}
		defer file.Close()
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				user := parts[0]
				rw := strings.ToLower(parts[1]) == "rw"
				userACL[user] = rw
			}
		}
		if err := scanner.Err(); err != nil {
			log.Fatal("reading passwd_acl: ", err)
		}
	}
	log.Printf("Loaded %q (htpasswd)", *passwdDb)
}

// emailSettingsEnabled returns true if the email mapping file is configured (so users can manage emails).
func emailSettingsEnabled() bool {
	return getPasswdEmailPath() != ""
}

// getPasswdEmailPath returns the path to the email mapping file. When -chroot and -passwd are set
// and -passwd_email is empty, defaults to /etc/wfm.emails (inside chroot).
func getPasswdEmailPath() string {
	if *passwdEmail != "" {
		return *passwdEmail
	}
	if *chrootDir != "" && *passwdDb != "" {
		return "/etc/wfm.emails"
	}
	return ""
}

// loadEmails loads optional email → username mappings. File format: one line per user,
// "username primary_email [secondary_email]". Both emails can be used for password reset.
func loadEmails() {
	path := getPasswdEmailPath()
	if path == "" {
		emailToUser = nil
		return
	}
	file, err := os.Open(path)
	if err != nil {
		log.Fatal("unable to open passwd_email file: ", err)
	}
	defer file.Close()
	m := make(map[string]string)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		user := parts[0]
		for _, email := range parts[1:] {
			e := strings.ToLower(strings.TrimSpace(email))
			if e != "" {
				m[e] = user
			}
		}
	}
	if err := scanner.Err(); err != nil {
		log.Fatal("reading passwd_email: ", err)
	}
	emailToUser = m
	log.Printf("Loaded %q (email mapping, %d entries)", path, len(emailToUser))
}

// reloadEmails re-reads the optional passwd_email file so changes are visible without restart.
func reloadEmails() {
	path := getPasswdEmailPath()
	if path == "" {
		return
	}
	file, err := os.Open(path)
	if err != nil {
		log.Printf("passwd_email reload: %v", err)
		return
	}
	defer file.Close()
	m := make(map[string]string)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		user := parts[0]
		for _, email := range parts[1:] {
			e := strings.ToLower(strings.TrimSpace(email))
			if e != "" {
				m[e] = user
			}
		}
	}
	if err := scanner.Err(); err != nil {
		log.Printf("passwd_email reload: %v", err)
		return
	}
	emailToUser = m
}

// getEmailsForUser returns primary and secondary email for the user (either may be empty).
func getEmailsForUser(username string) (primary, secondary string) {
	path := getPasswdEmailPath()
	if path == "" || username == "" {
		return "", ""
	}
	file, err := os.Open(path)
	if err != nil {
		return "", ""
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) >= 2 && parts[0] == username {
			primary = strings.TrimSpace(parts[1])
			if len(parts) >= 3 {
				secondary = strings.TrimSpace(parts[2])
			}
			return primary, secondary
		}
	}
	return "", ""
}

// updateUserEmail updates primary or secondary email for username in the email file.
// slot is "primary" or "secondary". Reloads in-memory mapping on success.
func updateUserEmail(username, slot, newEmail string) error {
	path := getPasswdEmailPath()
	if path == "" {
		return errors.New("email mapping file not configured")
	}
	newEmail = strings.TrimSpace(strings.ToLower(newEmail))
	if newEmail == "" && slot == "primary" {
		return errors.New("primary email cannot be empty")
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	lineSep := "\n"
	if strings.Contains(string(content), "\r\n") {
		lineSep = "\r\n"
	}
	lines := strings.Split(string(content), lineSep)
	updated := false
	for i, line := range lines {
		parts := strings.Fields(line)
		if len(parts) < 2 || parts[0] != username {
			continue
		}
		if slot == "primary" {
			sec := ""
			if len(parts) >= 3 {
				sec = parts[2]
			}
			lines[i] = username + " " + newEmail
			if sec != "" {
				lines[i] += " " + sec
			}
		} else {
			// secondary: allow empty to clear
			if newEmail == "" {
				lines[i] = username + " " + parts[1]
			} else {
				lines[i] = username + " " + parts[1] + " " + newEmail
			}
		}
		updated = true
		break
	}
	if !updated {
		if slot == "primary" && newEmail != "" {
			lines = append(lines, username+" "+newEmail)
		} else {
			return errors.New("user not found in email file")
		}
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, lineSep)), 0600); err != nil {
		return err
	}
	reloadEmails()
	return nil
}

// lookupUserByEmail returns username for the given email, if configured.
func lookupUserByEmail(email string) (string, bool) {
	if emailToUser == nil {
		return "", false
	}
	e := strings.ToLower(strings.TrimSpace(email))
	if e == "" {
		return "", false
	}
	u, ok := emailToUser[e]
	return u, ok
}

func userRW(username string) bool {
	if userACL == nil {
		return true
	}
	if rw, ok := userACL[username]; ok {
		return rw
	}
	return true
}

// reloadHtpasswd re-reads the htpasswd file so external changes (e.g. htpasswd CLI) are visible without restart.
func reloadHtpasswd() {
	if htpasswdFile == nil {
		return
	}
	if err := htpasswdFile.Reload(func(e error) { log.Printf("htpasswd reload: %v", e) }); err != nil {
		log.Printf("htpasswd reload: %v", err)
	}
}

// reloadACL re-reads the optional passwd_acl file so ACL changes are visible without restart.
func reloadACL() {
	if *passwdAcl == "" {
		return
	}
	userACL = make(map[string]bool)
	file, err := os.Open(*passwdAcl)
	if err != nil {
		log.Printf("passwd_acl reload: %v", err)
		return
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			user := parts[0]
			rw := strings.ToLower(parts[1]) == "rw"
			userACL[user] = rw
		}
	}
	if err := scanner.Err(); err != nil {
		log.Printf("passwd_acl reload: %v", err)
	}
}

func manageUsers() {
	switch flag.Arg(1) {
	case "list":
		listUsers()
	default:
		fmt.Println("usage: user list")
		fmt.Println("")
		fmt.Println("Users are managed with htpasswd(1). Create the password file with:")
		fmt.Println("  htpasswd -c /path/to/file username   # create file and add user")
		fmt.Println("  htpasswd /path/to/file username      # add or update user")
		fmt.Println("")
		fmt.Println("OpenBSD htpasswd uses bcrypt. Start wfm with: -passwd=/path/to/file")
		fmt.Println("Optional: -passwd_acl=/path/to/acl with lines 'username rw' or 'username ro'")
		fmt.Println("Optional: -passwd_email=/path/to/emails (or with -chroot default /etc/wfm.emails)")
		fmt.Println("  Email file format: one line per user 'username primary_email [secondary_email]'")
	}
}

func listUsers() {
	loadUsers()
	file, err := os.Open(*passwdDb)
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.Index(line, ":")
		if idx > 0 {
			user := line[:idx]
			rw := "rw"
			if userACL != nil {
				if ro, ok := userACL[user]; ok && !ro {
					rw = "ro"
				}
			}
			fmt.Printf("User: %q, %s\n", user, rw)
		}
	}
	if err := scanner.Err(); err != nil {
		log.Fatal(err)
	}
}

// updateUserPassword verifies currentPass and updates the htpasswd file with newPass for username. Reloads the in-memory file on success.
func updateUserPassword(username, currentPass, newPass string) error {
	if htpasswdFile == nil || *passwdDb == "" {
		return errors.New("password file not configured")
	}
	if !htpasswdFile.Match(username, currentPass) {
		return errors.New("wrong current password")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(newPass), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	content, err := os.ReadFile(*passwdDb)
	if err != nil {
		return err
	}
	lineSep := "\n"
	if strings.Contains(string(content), "\r\n") {
		lineSep = "\r\n"
	}
	lines := strings.Split(string(content), lineSep)
	updated := false
	for i, line := range lines {
		parts := strings.SplitN(strings.TrimSpace(line), ":", 2)
		if len(parts) == 2 && parts[0] == username {
			lines[i] = username + ":" + string(hash)
			updated = true
			break
		}
	}
	if !updated {
		return errors.New("user not found in password file")
	}
	if err := os.WriteFile(*passwdDb, []byte(strings.Join(lines, lineSep)), 0600); err != nil {
		return err
	}
	return htpasswdFile.Reload(func(e error) { log.Printf("htpasswd reload: %v", e) })
}

// resetUserPassword updates the htpasswd file with newPass for username without requiring the current password.
func resetUserPassword(username, newPass string) error {
	if htpasswdFile == nil || *passwdDb == "" {
		return errors.New("password file not configured")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(newPass), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	content, err := os.ReadFile(*passwdDb)
	if err != nil {
		return err
	}
	lineSep := "\n"
	if strings.Contains(string(content), "\r\n") {
		lineSep = "\r\n"
	}
	lines := strings.Split(string(content), lineSep)
	updated := false
	for i, line := range lines {
		parts := strings.SplitN(strings.TrimSpace(line), ":", 2)
		if len(parts) == 2 && parts[0] == username {
			lines[i] = username + ":" + string(hash)
			updated = true
			break
		}
	}
	if !updated {
		return errors.New("user not found in password file")
	}
	if err := os.WriteFile(*passwdDb, []byte(strings.Join(lines, lineSep)), 0600); err != nil {
		return err
	}
	return htpasswdFile.Reload(func(e error) { log.Printf("htpasswd reload: %v", e) })
}

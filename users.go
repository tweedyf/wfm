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

func userRW(username string) bool {
	if userACL == nil {
		return true
	}
	if rw, ok := userACL[username]; ok {
		return rw
	}
	return true
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

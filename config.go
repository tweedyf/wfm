package main

import (
	"bufio"
	"log"
	"os"
	"strings"
)

const defaultConfigPath = "/etc/wfm.conf"

// loadConfigArgs reads /etc/wfm.conf and returns flag-style arguments
// (e.g. "-addr=:443") so they can be prepended to os.Args. That yields
// precedence: defaults < config file < CLI. All flags (including chroot,
// setuid, chroot_users, edit_ext, upload_ext) can be set in the config file.
func loadConfigArgs(path string) []string {
	if path == "" {
		path = defaultConfigPath
	}
	f, err := os.Open(path)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("config: %v", err)
		}
		return nil
	}
	defer f.Close()
	log.Printf("Loading config from %q", path)

	var args []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// key=value or key value (value = rest of line)
		var key, val string
		if idx := strings.Index(line, "="); idx >= 0 {
			key = strings.TrimSpace(line[:idx])
			val = strings.TrimSpace(line[idx+1:])
		} else {
			fields := strings.SplitN(line, " ", 2)
			key = strings.TrimSpace(fields[0])
			if len(fields) == 2 {
				val = strings.TrimSpace(fields[1])
			}
		}
		if key == "" {
			continue
		}
		args = append(args, "-"+key+"="+val)
	}
	return args
}

// parseCommaSet returns a set (map[string]bool) from a comma-separated list.
// If toLower is true, keys are lowercased.
func parseCommaSet(s string, toLower bool) map[string]bool {
	set := make(map[string]bool)
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if toLower {
			part = strings.ToLower(part)
		}
		set[part] = true
	}
	return set
}

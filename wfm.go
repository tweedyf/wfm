// Web File Manager

package main

import (
	"crypto/tls"
	"flag"
	"log"
	"net"
	"net/http"
	"net/http/fcgi"
	"os"
	"os/user"
	"regexp"
	"strconv"
	"strings"
	"syscall"

	_ "github.com/breml/rootcerts"
	"github.com/gorilla/mux"
	"github.com/juju/ratelimit"
	"github.com/spf13/afero"
	"github.com/tenox7/tkvs"
	"golang.org/x/crypto/acme/autocert"
)

type multiString []string

var (
	vers       = "2.2.5"
	bindProto  = flag.String("proto", "tcp", "tcp, tcp4, tcp6, etc")
	bindAddr   = flag.String("addr", ":8080", "Listen address, eg: :443")
	bindExtra  = flag.String("addr_extra", "", "Extra non-TLS listener address, eg: :8081")
	chrootDir  = flag.String("chroot", "", "Directory to chroot to")
	suidUser   = flag.String("setuid", "", "Username or uid:gid pair to setuid to")
	allowRoot  = flag.Bool("allow_root", false, "allow to run as uid=0/root without setuid")
	siteName   = flag.String("site_name", "WFM", "local site name to display")
	siteDesc   = flag.String("site_desc", "Web File Manager", "site description")
	logFile    = flag.String("logfile", "", "Log file name (default stdout)")
	passwdDb    = flag.String("passwd", "", "htpasswd-style password file (create with htpasswd(1)); with -chroot, path is inside chroot (e.g. /etc/wfm.passwd)")
	passwdAcl   = flag.String("passwd_acl", "", "optional: file listing per-user rw/ro, one line per user: 'username rw' or 'username ro'")
	noPwdDbRW   = flag.Bool("nopass_rw", false, "allow read-write access if there is no password file")
	passwdEmail = flag.String("passwd_email", "", "optional: file mapping emails to usernames; with -chroot defaults to /etc/wfm.emails inside chroot; format: one line per user 'username primary_email [secondary_email]'")
	smtpServer   = flag.String("smtp_server", "127.0.0.1:25", "SMTP server for password reset and confirmation emails (ignored if sendmail_cmd is set)")
	smtpFrom    = flag.String("smtp_from", "wfm@localhost", "From address for password reset and confirmation emails (set in flags or wfm.conf)")
	smtpInsecure = flag.Bool("smtp_insecure_skip_verify", false, "skip TLS certificate verification for SMTP (use for local/dev servers with self-signed or hostname-only certs)")
	sendmailCmd  = flag.String("sendmail_cmd", "sendmail", "command to send mail (message piped to stdin with -t); set to empty string to use smtp_server instead")
	publicUrl    = flag.String("public_url", "", "public base URL for links in email (e.g. https://example.com/ or https://example.com/wfm); if empty, email links use path only")
	aboutRnt   = flag.Bool("about_runtime", true, "Display runtime info in About Dialog")
	showDot    = flag.Bool("show_dot", false, "show dot files and folders")
	listArc    = flag.Bool("list_archive_contents", false, "list contents of archives (expensive!)")
	rateLim    = flag.Int("rate_limit", 0, "rate limit for upload/download in MB/s, 0 no limit")
	formMaxMem = flag.Int64("form_maxmem", 10<<20, "maximum memory used for form parsing, increase for large uploads")
	prefix     = flag.String("prefix", "/:/", "Prefix for WFM access, /fsdir:/httppath eg.: /var/files:/myfiles")
	defLe      = flag.String("txt_le", "LF", "default line endings when editing text files")
	dumpHeader = flag.Bool("dump_headers", false, "dump headers sent by client")
	wfmFs      afero.Fs
	wfmPfx     string
	cacheCtl   = flag.String("cache_ctl", "no-cache", "HTTP Header Cache Control")
	acmFile    = flag.String("acm_file", "", "autocert cache, eg: /var/cache/wfm-acme.json")
	acmBind    = flag.String("acm_addr", "", "autocert manager listen address, eg: :80")
	acmWhlist  multiString // this flag set in main
	fastCgi    = flag.Bool("fastcgi", false, "enable FastCGI mode")
	f2bEnabled   = flag.Bool("f2b", true, "ban ip addresses on user/pass failures")
	f2bDump      = flag.String("f2b_dump", "", "enable f2b dump at this prefix, eg. /f2bdump (default no)")
	sessionSecret = flag.String("session_secret", "", "secret for signing session cookies (default: insecure default)")
	sessionMaxAge = flag.Int("session_max_age", 24*60*60, "session cookie max age in seconds (default 24h)")

	chrootUsers = flag.String("chroot_users", "admin", "comma-separated usernames that have access to chroot root; others are limited to a subdirectory named by username")
	editExt     = flag.String("edit_ext", "", "comma-separated file extensions that open in the web editor (e.g. txt,md,html); default empty means none")
	uploadExt   = flag.String("upload_ext", defaultUploadExt, "comma-separated file extensions allowed for upload")
)

var (
	chrootUsersSet map[string]bool // set of usernames with full chroot access
	editExtSet     map[string]bool // set of extensions that open in editor (lowercase)
	uploadExtSet   map[string]bool // set of extensions allowed for upload (lowercase)
)

const defaultUploadExt = "txt,log,csv,md,markdown,mdown,html,htm,xml,json,js,css,cfg,conf,ini,yaml,yml,rst,tex,text,pdf,png,jpg,jpeg,gif,webp,bmp,ico,tif,tiff,heif,heic,svg"

func userId(usr string) (int, int, error) {
	u, err := user.Lookup(usr)
	if err != nil {
		return 0, 0, err
	}
	ui, err := strconv.Atoi(u.Uid)
	if err != nil {
		return 0, 0, err
	}
	gi, err := strconv.Atoi(u.Gid)
	if err != nil {
		return 0, 0, err
	}
	return ui, gi, nil
}

func setUid(ui, gi int) error {
	if ui == 0 || gi == 0 {
		return nil
	}
	err := syscall.Setgid(gi)
	if err != nil {
		return err
	}
	err = syscall.Setuid(ui)
	if err != nil {
		return err
	}
	return nil
}

func (z *multiString) String() string {
	return "something"
}

func (z *multiString) Set(v string) error {
	*z = append(*z, v)
	return nil
}

func emit(s string, c int) string {
	o := strings.Builder{}
	for c > 0 {
		o.WriteString(s)
		c--
	}
	return o.String()
}

func noText(m map[string][]string) map[string][]string {
	o := make(map[string][]string)
	for k, v := range m {
		if k == "text" {
			continue
		}
		o[k] = v
	}
	return o
}

func atoiOrFatal(s string) int {
	i, err := strconv.Atoi(s)
	if err != nil {
		log.Fatal(err)
	}
	return i
}

// hasChrootAccess returns true if the username is in the chroot_users list (full access to chroot root).
func hasChrootAccess(username string) bool {
	return chrootUsersSet[username]
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.Printf("WFM %v Starting up", vers)

	// Precedence: defaults < /etc/wfm.conf < CLI. Prepend config file args so Parse() sees CLI last.
	if cfgArgs := loadConfigArgs(defaultConfigPath); len(cfgArgs) > 0 {
		os.Args = append(append([]string{os.Args[0]}, cfgArgs...), os.Args[1:]...)
	}

	flag.Var(&acmWhlist, "acm_host", "autocert manager allowed hostname (multi)")
	flag.Parse()

	chrootUsersSet = parseCommaSet(*chrootUsers, false)
	editExtSet = parseCommaSet(*editExt, true)
	uploadExtSet = parseCommaSet(*uploadExt, true)

	var err error

	if flag.Arg(0) == "user" {
		manageUsers()
		return
	}

	if *logFile != "" {
		lf, err := os.OpenFile(*logFile, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0644)
		if err != nil {
			log.Fatal(err)
		}
		defer lf.Close()
		log.SetOutput(lf)
	}

	// find uid/gid for setuid before chroot
	var suid, sgid int
	if *suidUser != "" {
		uidSm := regexp.MustCompile(`^(\d+):(\d+)$`).FindStringSubmatch(*suidUser)
		switch len(uidSm) {
		case 3:
			suid = atoiOrFatal(uidSm[1])
			sgid = atoiOrFatal(uidSm[2])
		default:
			suid, sgid, err = userId(*suidUser)
			if err != nil {
				log.Fatal("unable to find setuid user", err)
			}
		}
		log.Printf("Requested setuid for %q suid=%v sgid=%v", *suidUser, suid, sgid)
	}

	// run autocert manager before chroot/setuid
	acm := autocert.Manager{}
	if *bindAddr != "" && *acmFile != "" && len(acmWhlist) > 0 {
		acm.Prompt = autocert.AcceptTOS
		acm.Cache = tkvs.New(*acmFile, autocert.ErrCacheMiss)
		acm.HostPolicy = autocert.HostWhitelist(acmWhlist...)
		go http.ListenAndServe(*acmBind, acm.HTTPHandler(nil))
		log.Printf("Autocert enabled for %v", acmWhlist)
	}

	// chroot now
	if *chrootDir != "" {
		err := syscall.Chroot(*chrootDir)
		if err != nil {
			log.Fatal("chroot", err)
		}
		log.Printf("Chroot to %q", *chrootDir)
	}

	// listen/bind to port before setuid
	l, err := net.Listen(*bindProto, *bindAddr)
	if err != nil {
		log.Fatalf("unable to listen on %v: %v", *bindAddr, err)
	}
	log.Printf("Listening on %q", *bindAddr)

	// setuid now
	if *suidUser != "" {
		err = setUid(suid, sgid)
		if err != nil {
			log.Fatalf("unable to suid for %v: %v", *suidUser, err)
		}
		if !*allowRoot && os.Getuid() == 0 {
			log.Fatal("you probably dont want to run wfm as root, use --allow_root flag to force it")
		}
		log.Printf("Setuid UID=%d GID=%d", os.Geteuid(), os.Getgid())
	}

	// Load password file after chroot (and setuid) so -passwd path is resolved inside chroot.
	// When using -chroot, place the passwd file inside the chroot (e.g. -passwd=/etc/wfm.passwd → chroot/etc/wfm.passwd).
	if *passwdDb != "" {
		loadUsers()
		if getPasswdEmailPath() != "" {
			loadEmails()
		}
	}

	// rate limit setup
	if *rateLim != 0 {
		rlBu = ratelimit.NewBucketWithRate(float64(*rateLim<<20), 1<<10)
	}

	// http routing
	mux := mux.NewRouter()
	pfx := strings.Split(*prefix, ":")
	if len(pfx) != 2 || pfx[0][0] != '/' || pfx[1][0] != '/' {
		log.Fatal("--prefix must be in format '/dir:/path'")
	}
	log.Printf("Prefix fs=%v uri=%v", pfx[0], pfx[1])
	wfmFs = afero.NewOsFs()
	if pfx[0] != "/" {
		wfmFs = afero.NewBasePathFs(wfmFs, pfx[0])
	}
	wfmPfx = pfx[1]
	
	// Serve static files (embedded fallback)
	staticPrefix := strings.TrimSuffix(wfmPfx, "/") + "/static/"
	mux.PathPrefix(staticPrefix).Handler(http.StripPrefix(staticPrefix, http.HandlerFunc(serveStatic)))
	
	mux.PathPrefix(wfmPfx).HandlerFunc(wfmMain)
	if *f2bDump != "" {
		mux.HandleFunc(*f2bDump, dumpf2b)
	}

	if *bindExtra != "" {
		log.Printf("Listening (extra) on %q", *bindAddr)
		go http.ListenAndServe(*bindExtra, mux)
	}
	switch {
	case *acmBind != "" && *acmFile != "" && len(acmWhlist) > 0:
		https := &http.Server{
			Addr:      *bindAddr,
			Handler:   mux,
			TLSConfig: &tls.Config{GetCertificate: acm.GetCertificate},
		}
		log.Printf("Starting HTTPS TLS Server")
		err = https.ServeTLS(l, "", "")
	case *fastCgi:
		log.Print("Starting FastCGI Server")
		fcgi.Serve(l, http.DefaultServeMux)
	default:
		log.Printf("Starting HTTP Server")
		err = http.Serve(l, mux)
	}
	if err != nil {
		log.Fatal(err)
	}
}

package main

import (
	"fmt"
	"html"
	"log"
	"net/http"
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

func header(w http.ResponseWriter, uDir, sort, extraCSS string, modern bool) {
	eDir := html.EscapeString(uDir)
	w.Header().Set("Content-Type", "text/html")
	w.Header().Set("Cache-Control", *cacheCtl)
	w.Write([]byte(`<!DOCTYPE HTML PUBLIC "-//W3C//DTD HTML 4.01 Transitional//EN" "http://www.w3.org/TR/html4/loose.dtd">
<HTML LANG="en">
<HEAD>
<META HTTP-EQUIV="Content-Type" CONTENT="text/html;charset=` + charset[modern] + `">
<META HTTP-EQUIV="Content-Language" CONTENT="en-US">
<META HTTP-EQUIV="google" CONTENT="notranslate">
<META HTTP-EQUIV="charset" CONTENT="` + charset[modern] + `">
<META HTTP-EQUIV="encoding" CONTENT="` + charset[modern] + `">
<META NAME="viewport" CONTENT="width=device-width">
<META NAME="description" CONTENT="` + *siteDesc + `">
<LINK REL="icon" TYPE="image/x-icon" HREF="/favicon.ico">
<LINK REL="shortcut icon" HREF="/favicon.ico?">
<TITLE>` + *siteName + ` : ` + eDir + `</TITLE>
<STYLE TYPE="text/css"><!--
	A:link {text-decoration: none; color:#0000CE; }
	A:visited {text-decoration: none; color:#0000CE; }
	A:active {text-decoration: none; color:#FF0000; }
	A:hover {text-decoration: none; background-color: #FF8000; color: #FFFFFF; }
	html, body, table { margin:0px; padding:0px; border:none;  }
	td, th { font-family: Tahoma, Arial, Geneva, sans-serif; font-size:13px; margin:0px; padding:` + padding[modern] + `; border:none; }
	input { font-family: Tahoma, Arial, Geneva, sans-serif; font-size:13px; }
	.thov tr:hover { background-color: #FF8000; color: #FFFFFF; }
	.tbr { border-width: 1px; border-style: solid solid solid solid; border-color: #AAAAAA #555555 #555555 #AAAAAA; }
	.nb { border-style:none; background-color: #EEEEEE; }
	.drop-overlay { position:fixed; inset:0; background:rgba(0,114,198,0.15); border:3px dashed #0072c6; z-index:9999; display:none; align-items:center; justify-content:center; pointer-events:none; }
	body.drag-over .drop-overlay { display:flex; }
	.drop-overlay span { background:#fff; padding:12px 24px; border-radius:4px; font-weight:bold; color:#0072c6; }
	` + extraCSS + `
--></STYLE>
</HEAD>
<BODY BGCOLOR="#FFFFFF">
<DIV CLASS="drop-overlay" ID="dropOverlay"><SPAN>Drop file to upload</SPAN></DIV>
<FORM ACTION="` + wfmPfx + `" METHOD="POST" ENCTYPE="multipart/form-data" ID="wfmForm">
<INPUT TYPE="hidden" NAME="dir" VALUE="` + eDir + `">
<INPUT TYPE="hidden" NAME="sort" VALUE="` + sort + `">
`))
}

func footer(w http.ResponseWriter) {
	w.Write([]byte(`
<SCRIPT>
(function(){
	var form=document.getElementById("wfmForm");
	if(!form) return;
	var dirIn=form.querySelector('input[name="dir"]');
	var sortIn=form.querySelector('input[name="sort"]');
	if(!dirIn||!sortIn) return;
	function prevent(e){ e.preventDefault(); e.stopPropagation(); }
	function onDragOver(e){ prevent(e); if(e.dataTransfer.types.indexOf("Files")>=0) document.body.classList.add("drag-over"); }
	function onDragLeave(e){ prevent(e); if(!e.relatedTarget||!document.body.contains(e.relatedTarget)) document.body.classList.remove("drag-over"); }
	function onDrop(e){
		prevent(e);
		document.body.classList.remove("drag-over");
		var files=e.dataTransfer.files;
		if(!files||!files.length) return;
		var dir=dirIn.value, sort=sortIn.value||"", action=form.action;
		var upload=function(i){
			if(i>=files.length){ if(i>0) location.reload(); return; }
			var fd=new FormData();
			fd.append("dir",dir);
			fd.append("sort",sort);
			fd.append("upload","1");
			fd.append("filename",files[i]);
			fetch(action,{method:"POST",body:fd,redirect:"manual"}).then(function(r){
				if(r.type==="opaqueredirect"||(r.status>=300&&r.status<400)){
					if(i+1>=files.length) location.reload();
					else upload(i+1);
				}else{
					upload(i+1);
				}
			}).catch(function(){ upload(i+1); });
		};
		upload(0);
	}
	document.body.addEventListener("dragover",onDragOver);
	document.body.addEventListener("dragenter",onDragOver);
	document.body.addEventListener("dragleave",onDragLeave);
	document.body.addEventListener("drop",onDrop);
})();
</SCRIPT>
</FORM></BODY></HTML>
`))
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

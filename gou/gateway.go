/*
 * Copyright (c) 2015, Shinya Yagyu
 * All rights reserved.
 * Redistribution and use in source and binary forms, with or without
 * modification, are permitted provided that the following conditions are met:
 *
 * 1. Redistributions of source code must retain the above copyright notice,
 *    this list of conditions and the following disclaimer.
 * 2. Redistributions in binary form must reproduce the above copyright notice,
 *    this list of conditions and the following disclaimer in the documentation
 *    and/or other materials provided with the distribution.
 * 3. Neither the name of the copyright holder nor the names of its
 *    contributors may be used to endorse or promote products derived from this
 *    software without specific prior written permission.
 *
 * THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS "AS IS"
 * AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE
 * IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE
 * ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT HOLDER OR CONTRIBUTORS BE
 * LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR
 * CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF
 * SUBSTITUTE GOODS OR SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS
 * INTERRUPTION) HOWEVER CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN
 * CONTRACT, STRICT LIABILITY, OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE)
 * ARISING IN ANY WAY OUT OF THE USE OF THIS SOFTWARE, EVEN IF ADVISED OF THE
 * POSSIBILITY OF SUCH DAMAGE.
 */

package gou

import (
	"bytes"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"path"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/text/language"
)

//Connection Counter.
type counter struct {
	N     int
	mutex sync.Mutex
}

func (c *counter) increment() {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.N++
}

func (c *counter) decrement() {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.N--
}

type message map[string]string

func newMessage(file string) message {
	var m message
	re := regexp.MustCompile("^#$")
	err := eachLine(file, func(line string, i int) error {
		line = strings.Trim(line, "\r\n")
		var err error
		if !re.MatchString(line) {
			buf := strings.Split(line, "<>")
			if len(buf) == 2 {
				buf[1], err = url.QueryUnescape(buf[1])
				m[buf[0]] = buf[1]
			}
		}
		return err
	})
	if err != nil {
		log.Println(file, err)
	}
	return m
}

func (m message) get(k string) string {
	if v, exist := m[k]; exist {
		return v
	}
	return ""
}

func searchMessage(acceptLanguage string) message {
	lang := make([]string, 0)
	if acceptLanguage != "" {
		tags, _, err := language.ParseAcceptLanguage(acceptLanguage)
		if err != nil {
			log.Println(err)
		} else {
			for _, tag := range tags {
				lang = append(lang, tag.String())
			}
		}
	}
	lang = append(lang, default_language)
	for _, l := range lang {
		slang := strings.Split(l, "-")[0]
		for _, j := range []string{l, slang} {
			file := path.Join(file_dir, "message-"+j+".txt")
			if isFile(file) {
				return newMessage(file)
			}
		}
	}
	return nil
}

type DefaultVariable struct {
	CGI         *cgi
	Environment http.Header
	UA          string
	Message     message
	Lang        string
	Aappl       map[string]string
	GatewayCgi  string
	ThreadCgi   string
	AdminCgi    string
	RootPath    string
	Types       []string
	Isadmin     bool
	Isfriend    bool
	Isvisitor   bool
	Dummyquery  int64
}

func (d *DefaultVariable) Add(a, b int) int {
	return a + b
}
func (d *DefaultVariable) Mul(a, b int) int {
	return a * b
}
func (d *DefaultVariable) ToKB(a int) float64 {
	return float64(a) / 1024
}
func (d *DefaultVariable) ToMB(a int) float64 {
	return float64(a) / (1024 * 1024)
}
func (d *DefaultVariable) Localtime(stamp int64) string {
	return time.Unix(stamp, 0).Format("2006-01-02 15:04")
}
func (d *DefaultVariable) StrEncode(query string) string {
	return strEncode(query)
}

func (d *DefaultVariable) Escape(msg string) string {
	return escape(msg)
}
func (d *DefaultVariable) EscapeSimple(msg string) string {
	return cgiEscape(msg, false)
}
func (d *DefaultVariable) EscapeSpace(msg string) string {
	return escapeSpace(msg)
}
func (d *DefaultVariable) EscapeJS(msg string) string {
	return strings.Replace(strings.Replace(msg, "\"", "\\\"", -1), "]]>", "", -1)
}
func (d *DefaultVariable) FileDecode(query, t string) string {
	q := strings.Split(query, "_")
	if len(q) < 2 {
		return t
	}
	return q[0]
}

func (c *DefaultVariable) makeGatewayLink(cginame, command string) string {
	g := struct {
		CGIname     string
		Command     string
		Description string
	}{
		CGIname:     cginame,
		Command:     command,
		Description: c.Message.get("desc_" + command),
	}
	var doc bytes.Buffer
	renderTemplate("gateway_link", g, &doc)
	return doc.String()
}

type cgi struct {
	m          message
	host       string
	filter     string
	strFilter  string
	tag        string
	strTag     string
	remoteaddr string
	isAdmin    bool
	isFriend   bool
	jc         *jsCache
	isVisitor  bool
	req        *http.Request
	wr         http.ResponseWriter
}

func match(re string, val string) bool {
	reg, err := regexp.Compile(re)
	if err != nil {
		return reg.MatchString(val)
	}
	return false
}

func newCGI(w http.ResponseWriter, r *http.Request) *cgi {
	c := &cgi{
		remoteaddr: r.RemoteAddr,
		jc:         newJsCache(absDocroot),
		wr:         w,
	}
	c.m = newMessage(r.Header.Get("Accept-Language"))
	c.isAdmin = match(re_admin, c.remoteaddr)
	c.isFriend = match(re_friend, c.remoteaddr)
	c.isVisitor = match(re_visitor, c.remoteaddr)
	c.req = r

	return c
}

func (c *cgi) makeListItem(ca *cache, remove bool, target string, search bool) string {
	x, _ := fileDecode(ca.datfile)
	if x == "" {
		return ""
	}
	y := strEncode(x)
	if c.filter != "" && !strings.Contains(c.filter, strings.ToLower(x)) {
		return ""
	}
	if c.tag != "" {
		cacheTags := make([]*tag, 0)
		matchtag := false
		cacheTags = append(cacheTags, ca.tags.tags...)
		if target == "recent" {
			cacheTags = append(cacheTags, ca.sugtags.tags...)
		}
		for _, t := range cacheTags {
			if strings.ToLower(t.tagstr) == c.tag{
				matchtag = true
				break
			}
		}
		if !matchtag {
			return ""
		}
	}
	x = escapeSpace(x)
	var strOpts string
	if search {
		strOpts = "?search_new_file=yes"
	}
	sugtags := make([]*tag, 0)
	if target == "recent" {
		strTags := make([]string, ca.tags.Len())
		for i, v := range ca.tags.tags {
			strTags[i] = strings.ToLower(v.tagstr)
		}
		for _, st := range ca.sugtags.tags {
			if !hasString(stringSlice(strTags), strings.ToLower(st.tagstr)) {
				sugtags = append(sugtags, st)
			}
		}
	}
	var doc bytes.Buffer
	g := struct {
		*DefaultVariable
		cache    *cache
		title    string
		strTitle string
		tags     *tagList
		sugtags  []*tag
		target   string
		remove   bool
		strOpts  string
	}{
		c.makeDefaultVariable(),
		ca,
		x,
		y,
		ca.tags,
		sugtags,
		target,
		remove,
		strOpts,
	}
	renderTemplate("footer", g, &doc)
	return doc.String()
}

func (c *cgi) extension(suffix string, useMerged bool) []string {
	filename := make([]string, 0)
	var merged string
	eachFiles(absDocroot, func(f os.FileInfo) error {
		i := f.Name()
		if strings.HasSuffix(i, "."+suffix) && (!strings.HasPrefix(i, ".") || strings.HasPrefix(i, "_")) {
			filename = append(filename, i)
		} else {
			if useMerged && i == "__merged."+suffix {
				merged = i
			}
		}
		return nil
	})
	if merged != "" {
		return []string{merged}
	}
	sort.Strings(filename)
	return filename
}

type Menubar struct {
	*DefaultVariable
	Id  string
	RSS string
}

func (c *cgi) makeMenubar(id, rss string) *Menubar {
	g := &Menubar{
		DefaultVariable: c.makeDefaultVariable(),
		Id:              id,
		RSS:             rss,
	}
	return g
}

type Footer struct {
	*DefaultVariable
	Menu Menubar
}

func (c *cgi) footer(menubar *Menubar) {
	g := &Footer{
		DefaultVariable: c.makeDefaultVariable(),
		Menu:            *menubar,
	}
	renderTemplate("footer", g, c.wr)
}

func (c *cgi) rfc822Time(stamp int64) string {
	return time.Unix(stamp, 0).Format("2006-01-02 15:04:05")
}

func (c *cgi) printParagraph(contents string) {
	g := struct {
		*DefaultVariable
		Contents string
	}{
		DefaultVariable: c.makeDefaultVariable(),
		Contents:        contents,
	}
	renderTemplate("paragraph", g, c.wr)
}

type Header struct {
	*DefaultVariable
	Title     string
	StrTitle  string
	RSS       string
	DenyRobot bool
	Mergedjs  []string
	JS        *jsCache
	CSS       []string
	Menu      Menubar
}

func (c *cgi) header(title, rss string, cookie *http.Cookie, denyRobot bool, menu *Menubar) {
	if rss == "" {
		rss = gateway_cgi + "/rss"
	}
	c.req.ParseForm()
	var js []string
	if c.req.FormValue("__debug_js") != "" {
		js = c.extension("js", false)
	} else {
		c.jc.update()
	}
	h := &Header{
		DefaultVariable: c.makeDefaultVariable(),
		Title:           title,
		StrTitle:        strEncode(title),
		RSS:             rss,
		DenyRobot:       denyRobot,
		Mergedjs:        js,
		CSS:             c.extension("css", false),
		Menu:            *menu,
	}
	http.SetCookie(c.wr, cookie)
	renderTemplate("header", h, c.wr)
}

func (c *cgi) resAnchor(id, appli string, title string, absuri bool) string {
	title = strEncode(title)
	var prefix, innerlink string
	if absuri {
		prefix = "http://" + c.host
	} else {
		innerlink = " class=\"innderlink\""
	}
	return fmt.Sprintf("<a href=\"%s%s%s%s/%s\"%s>", prefix, appli, query_separator, title, id, innerlink)
}

func group(str string, i int, loc []int) string {
	return str[loc[i]:loc[i+1]]
}

func (c *cgi) htmlFormat(plain, appli string, title string, absuri bool) string {
	buf := strings.Replace(plain, "<br>", "\n", -1)
	buf = strings.Replace(buf, "\t", "        ", -1)
	buf = escape(buf)
	reg := regexp.MustCompile("https?://[^\\x00-\\x20\"'()<>\\[\\]\\x7F-\\xFF]{2,}")
	buf = reg.ReplaceAllString(buf, "<a href=\"\\g<0>\">\\g<0></a>")
	reg = regexp.MustCompile("(&gt;&gt;)([0-9a-f]{8})")
	id := reg.ReplaceAllString(buf, "\\2")
	buf = reg.ReplaceAllString(buf, c.resAnchor(id, appli, title, absuri)+"\\g<0></a>")

	var tmp string
	reg = regexp.MustCompile("\\[\\[([^<>]+?)\\]\\]")
	for buf != "" {
		if m := reg.FindStringSubmatchIndex(buf); m != nil {
			tmp += buf[:m[0]]
			tmp += c.bracketLink(group(buf, 2, m), appli, absuri)
			buf = buf[m[1]:]
		} else {
			tmp += buf
			break
		}
	}
	return escapeSpace(tmp)
}

func (c *cgi) bracketLink(link, appli string, absuri bool) string {

	var prefix string
	if absuri {
		prefix = "http://" + c.host
	}
	reg := regexp.MustCompile("^/(thread)/([^/]+)/([0-9a-f]{8})$")
	if m := reg.FindStringSubmatch(link); m != nil {
		url := prefix + thread_cgi + query_separator + strEncode(m[2]) + "/" + m[3]
		return "<a href=\"" + url + "\" class=\"reclink\">[[" + link + "]]</a>"
	}

	reg = regexp.MustCompile("^/(thread)/([^/]+)$")
	if m := reg.FindStringSubmatch(link); m != nil {
		uri := prefix + application[m[1]] + query_separator + strEncode(m[2])
		return "<a href=\"" + uri + "\">[[" + link + "]]</a>"
	}

	reg = regexp.MustCompile("^([^/]+)/([0-9a-f]{8})$")
	if m := reg.FindStringSubmatch(link); m != nil {
		uri := prefix + appli + query_separator + strEncode(m[1]) + "/" + m[2]
		return "<a href=\"" + uri + "\" class=\"reclink\">[[" + link + "]]</a>"
	}

	reg = regexp.MustCompile("^([^/]+)$")
	if m := reg.FindStringSubmatch(link); m != nil {
		uri := prefix + appli + query_separator + strEncode(m[1])
		return "<a href=\"" + uri + "\">[[" + link + "]]</a>"
	}
	return "[[" + link + "]]"
}

func (c *cgi) removeFileForm(ca *cache, title string) {
	s := struct {
		Cache *cache
		Title string
	}{
		ca,
		title,
	}
	renderTemplate("remove_file_form", s, c.wr)
}

func (c *cgi) mchUrl() string {
	path := "/2ch/subject.txt"
	if !enable2ch {
		return ""
	}
	if server_name != "" {
		return "//" + server_name + path
	}
	reg := regexp.MustCompile(":\\d+")
	host := reg.ReplaceAllString(c.req.Host, "")
	if host == "" {
		return ""
	}
	return fmt.Sprintf("//%s:%d%s", host, dat_port, path)
}

type mchCategory struct {
	url  string
	text string
}

func (c *cgi) mchCategories() []*mchCategory {
	categories := make([]*mchCategory, 0)
	if !enable2ch {
		return categories
	}
	mchUrl := c.mchUrl()
	eachLine(run_dir+"/tag.txt", func(line string, i int) error {
		tag := strings.TrimRight(line, "\r\n")
		catUrl := strings.Replace(mchUrl, "2ch", fileEncode("2ch", tag), -1)
		categories = append(categories, &mchCategory{
			url:  catUrl,
			text: tag,
		})
		return nil
	})
	return categories
}

func (c *cgi) printJump(next string) {
	s := struct {
		DefaultVariable *DefaultVariable
		Next            string
	}{
		c.makeDefaultVariable(),
		next,
	}
	renderTemplate("jump", s, c.wr)
}

func (c *cgi) print302(next string) {
	c.header("Loading...", "", nil, false, nil)
	c.printJump(next)
	c.footer(nil)
}
func (c *cgi) print403(next string) {
	c.header(c.m.get("403"), "", nil, true, nil)
	c.printParagraph(c.m["403_body"])
	c.printJump(next)
	c.footer(nil)
}
func (c *cgi) print404(ca *cache, id string) {
	c.header(c.m.get("404"), "", nil, true, nil)
	c.printParagraph(c.m["404_body"])
	if ca != nil {
		c.removeFileForm(ca, "")
	}
	c.footer(nil)
}
func touch(fname string) error {
	f, err := os.Create(fname)
	if err != nil {
		log.Println(err)
		return err
	} else {
		f.Close()
	}
	return nil
}

func (c *cgi) lock() bool {
	var lockfile string
	if c.isAdmin {
		lockfile = admin_search
	} else {
		lockfile = search_lock
	}
	if !isFile(lockfile) {
		touch(lockfile)
		return true
	}
	s, err := os.Stat(lockfile)
	if err != nil {
		log.Println(err)
		return false
	}
	if s.ModTime().Add(search_timeout).Before(time.Now()) {
		touch(lockfile)
		return true
	}
	return false
}

func (c *cgi) unlock() error {
	var lockfile string
	if c.isAdmin {
		lockfile = admin_search
	} else {
		lockfile = search_lock
	}
	err := os.Remove(lockfile)
	if err != nil {
		log.Println(err)
	}
	return err
}

func (c *cgi) getCache(ca *cache) bool {
	result := ca.search(nil, nil)
	c.unlock()
	return result
}

func (c *cgi) printNewElementForm() {
	if !c.isAdmin && !c.isFriend {
		return
	}
	s := struct {
		DefaultVariable *DefaultVariable
		datfile         string
		cginame         string
	}{
		c.makeDefaultVariable(),
		"",
		gateway_cgi,
	}
	renderTemplate("new_element_form", s, c.wr)
}

type attached struct {
	filename string
	data     []byte
}

func (c *cgi) parseAttached() (*attached, error) {
	c.req.ParseForm()
	err := c.req.ParseMultipartForm(int64(record_limit) << 10)
	if err != nil {
		return nil, err
	}
	attach := c.req.MultipartForm
	if len(attach.File) > 0 {
		filename := attach.Value["filename"][0]
		fpStrAttach := attach.File[filename][0]
		f, err := fpStrAttach.Open()
		defer f.Close()
		if err != nil {
			return nil, err
		}
		var strAttach = make([]byte, record_limit<<10)
		_, err = f.Read(strAttach)
		if err == nil || err.Error() != "EOF" {
			c.header(c.m["big_file"], "", nil, true, nil)
			c.footer(nil)
			return nil, err
		}
		return &attached{
			filename: filename,
			data:     strAttach,
		}, nil
	}
	return nil, errors.New("attached file not found")
}

func (c *cgi) doPost() string {
	attached, attachedErr := c.parseAttached()
	if attachedErr != nil {
		log.Println(attachedErr)
	}
	guessSuffix := "txt"
	if attachedErr == nil {
		e := path.Ext(attached.filename)
		if e != "" {
			guessSuffix = strings.ToLower(e)
		}
	}

	suffix := c.req.FormValue("suffix")
	switch {
	case suffix == "" || suffix == "AUTO":
		suffix = guessSuffix
	case strings.HasPrefix(suffix, "."):
		suffix = suffix[1:]
	}
	suffix = strings.ToLower(suffix)
	reg := regexp.MustCompile("[^0-9A-Za-z]")
	suffix = reg.ReplaceAllString(suffix, "")

	ca := newCache(c.req.FormValue("file"), nil, nil)
	stamp := time.Now().Unix()
	body := make(map[string]string)
	if value := c.req.FormValue("body"); value != "" {
		body["body"] = escape(value)
	}

	if attachedErr == nil {
		body["attach"] = string(attached.data)
		body["suffix"] = strings.Trim(suffix, "\r\n")
	}
	if len(body) == 0 {
		c.header(c.m["null_article"], "", nil, true, nil)
		c.footer(nil)
		return ""
	}
	rec := newRecord(ca.datfile, "")
	passwd := c.req.FormValue("passwd")
	id := rec.build(stamp, body, passwd)

	proxyClient := c.req.Header.Get("X_FORWARDED_FOR")
	log.Printf("post %s/%d_%s from %s/%s\n", ca.datfile, stamp, id, c.remoteaddr, proxyClient)

	if len(rec.recstr) > record_limit<<10 {
		c.header(c.m["big_file"], "", nil, true, nil)
		c.footer(nil)
		return ""
	}
	if spamCheck(rec.recstr) {
		c.header(c.m["spam"], "", nil, true, nil)
		c.footer(nil)
		return ""
	}

	if ca.exists() {
		ca.addData(rec, true)
		ca.syncStatus()
	} else {
		c.print404(nil, "")
		return ""
	}

	if c.req.FormValue("dopost") != "" {
		queue.append(ca.datfile, stamp, id, nil)
		go queue.run()
	}

	return id[:8]

}

func (c *cgi) printIndexList(cl *cacheList, target string, footer bool, searchNewFile bool) {
	s := struct {
		DefaultVariable *DefaultVariable
		target          string
		filter          string
		tag             string
		taglist         *userTagList
		chachelist      *cacheList
		searchNewFile   bool
	}{
		c.makeDefaultVariable(),
		target,
		c.strFilter,
		c.strTag,
		newUserTagList(),
		cl,
		searchNewFile,
	}
	renderTemplate("index_list", s, c.wr)
	if footer {
		c.printNewElementForm()
		c.footer(nil)
	}
}

func (c *cgi) checkVisitor() bool {
	return c.isAdmin || c.isFriend || c.isVisitor
}

func (c *cgi) makeDefaultVariable() *DefaultVariable {
	return &DefaultVariable{
		CGI:         c,
		Environment: c.req.Header,
		UA:          c.req.Header.Get("USER_AGENT"),
		Message:     c.m,
		Lang:        c.m["lang"],
		Aappl:       application,
		GatewayCgi:  gateway_cgi,
		ThreadCgi:   thread_cgi,
		AdminCgi:    admin_cgi,
		RootPath:    root_path,
		Types:       types,
		Isadmin:     c.isAdmin,
		Isfriend:    c.isFriend,
		Isvisitor:   c.isVisitor,
		Dummyquery:  time.Now().Unix(),
	}
}

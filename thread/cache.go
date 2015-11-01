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

package thread

import (
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/shingetsu-gou/shingetsu-gou/node"
	"github.com/shingetsu-gou/shingetsu-gou/util"
)

var (
	cacheMap = make(map[string]sync.Pool)
	errSpam  = errors.New("this is spam")
	errGet   = errors.New("cannot get data")
	//CacheCfg is config for Cache struct.it must be set before using it.
	CacheCfg *CacheConfig
)

//CacheInfo represents size/len/velocity of cache.
type CacheInfo struct {
	Size     int64 //size of total records
	Len      int   //# of records
	Velocity int   //# of new records in one day
	Stamp    int64 //stamp of newest record
}

//CacheConfig is config for Cache struct.
type CacheConfig struct {
	CacheDir          string
	RecordLimit       int
	SyncRange         int64
	GetRange          int64
	NodeManager       *node.Manager
	UserTag           *UserTag
	SuggestedTagTable *SuggestedTagTable
	RecentList        *RecentList
	Fmutex            *sync.RWMutex
}

//Cache represents cache of one file.
type Cache struct {
	*CacheConfig
	Datfile string
	tags    Tagslice //made by the user
	mutex   sync.RWMutex
}

//NewCache read tag files to set and returns cache obj.
//it uses sync.pool to ensure that only one cache obj exists for one datfile.
//and garbage collected when not used.
func NewCache(datfile string) *Cache {
	p, exist := cacheMap[datfile]
	if !exist {
		p.New = func() interface{} {
			c := &Cache{
				Datfile:     datfile,
				CacheConfig: CacheCfg,
			}
			c.tags = loadTagslice(path.Join(c.datpath(), "tag.txt"))
			return c
		}
	}
	c := p.Get().(*Cache)
	p.Put(c)
	return c
}

//AddTags add user tag list from vals.
func (c *Cache) AddTags(vals []string) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.tags.addString(vals)
}

//SetTags sets user tag list from vals.
func (c *Cache) SetTags(vals []string) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.tags = newTagslice(vals)
}

//LenTags returns # of set user tag.
func (c *Cache) LenTags() int {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	return c.tags.Len()
}

//TagString returns string of user tag.
func (c *Cache) TagString() string {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	return c.tags.string()
}

//GetTagstrSlice returns tagstr slice of user tag.
func (c *Cache) GetTagstrSlice() []string {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	return c.tags.GetTagstrSlice()
}

//GetTags returns copy of usertags.
func (c *Cache) GetTags() Tagslice {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	ts := make([]*Tag, c.tags.Len())
	copy(ts, c.tags)
	return Tagslice(ts)
}

//HasTagstr returns true if tag has tagstr.
func (c *Cache) HasTagstr(tagstr string) bool {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	return c.tags.HasTagstr(tagstr)
}

//HasTag returns true if cache has tagstr=board tag in usertag or sugtag.
func (c *Cache) HasTag(board string) bool {
	if c.SuggestedTagTable.HasTagstr(c.Datfile, board) {
		return true
	}
	return c.HasTagstr(board)
}

//dathash returns datfile itself is type=asis.
func (c *Cache) dathash() string {
	return util.FileHash(c.Datfile)
}

//datpath returns real file path of this cache.
func (c *Cache) datpath() string {
	return path.Join(c.CacheDir, c.dathash())
}

//RecentStamp  returns time of getting by /recent.
func (c *Cache) RecentStamp() int64 {
	n := c.RecentList.Newest(c.Datfile)
	if n == nil {
		return c.ReadInfo().Stamp
	}
	return n.Stamp
}

//ReadInfo reads cache info from disk and returns #,velocity, and total size.
func (c *Cache) ReadInfo() *CacheInfo {
	c.Fmutex.RLock()
	defer c.Fmutex.RUnlock()
	d := path.Join(c.datpath(), "record")
	if !util.IsDir(d) {
		return nil
	}
	ci := &CacheInfo{}
	err := util.EachFiles(d, func(dir os.FileInfo) error {
		stamp, err := strconv.ParseInt(strings.Split(dir.Name(), "_")[0], 10, 64)
		if err != nil {
			log.Println(err)
			return nil
		}
		if ci.Stamp < stamp {
			ci.Stamp = stamp
		}
		if time.Unix(stamp, 0).After(time.Now().Add(-7 * 24 * time.Hour)) {
			ci.Velocity++
		}
		ci.Size += dir.Size()
		ci.Len++
		return nil
	})
	if err != nil {
		log.Fatal(err)
	}
	return ci
}

//LoadRecords loads and returns record maps from the disk .
func (c *Cache) LoadRecords() RecordMap {
	c.Fmutex.RLock()
	defer c.Fmutex.RUnlock()
	r := path.Join(c.datpath(), "record")
	if !util.IsDir(r) {
		return nil
	}
	if !c.Exists() {
		return nil
	}
	recs := make(map[string]*Record)
	err := util.EachFiles(r, func(f os.FileInfo) error {
		recs[f.Name()] = NewRecord(c.Datfile, f.Name())
		return nil
	})
	if err != nil {
		log.Println(err, c.datpath())
	}
	return RecordMap(recs)
}

//HasRecord return true if  cache has more than one records or removed records.
func (c *Cache) HasRecord() bool {
	c.Fmutex.RLock()
	defer c.Fmutex.RUnlock()
	f, err := ioutil.ReadDir(path.Join(c.datpath(), "record"))
	if err != nil {
		return false
	}
	removed := path.Join(c.datpath(), "removed")
	d, err := ioutil.ReadDir(removed)
	return len(f) > 0 || (err == nil && len(d) > 0)
}

//SyncTag saves usertags to files.
func (c *Cache) SyncTag() {
	c.Fmutex.Lock()
	defer c.Fmutex.Unlock()
	c.mutex.RLock()
	c.mutex.RUnlock()
	c.tags.sync(path.Join(c.datpath(), "tag.txt"))
}

//SetupDirectories make necessary dirs.
func (c *Cache) SetupDirectories() {
	c.Fmutex.Lock()
	defer c.Fmutex.Unlock()
	for _, d := range []string{"", "/attach", "/record", "/removed"} {
		di := path.Join(c.datpath(), d)
		if !util.IsDir(di) {
			err := os.Mkdir(di, 0755)
			if err != nil {
				log.Fatal(err)
			}
		}
	}
}

//checkData makes records from res and checks its records meets condisions of args.
//adds the rec to cache if meets conditions.
//if spam or big data, remove the rec from disk.
//returns count of added records to the cache and spam/getting error.
func (c *Cache) checkData(res []string, stamp int64, id string, begin, end int64) (int, error) {
	var err error
	count := 0
	for _, i := range res {
		r := NewRecord(c.Datfile, "")
		if er := r.parse(i); er == nil && r.meets(i, stamp, id, begin, end) {
			count++
			if len(i) > c.RecordLimit*1024 || r.IsSpam() {
				err = errSpam
				log.Printf("warning:%s/%s:too large or spam record", c.Datfile, r.Idstr())
				r.Sync()
				errr := r.Remove()
				if errr != nil {
					log.Println(errr)
				}
			} else {
				r.Sync()
			}
		} else {
			log.Println("warning::broken record", c.Datfile, i)
		}
	}
	if count == 0 {
		return 0, errGet
	}
	return count, err
}

//checkAttach checks files attach dir and if corresponding records
//don't exist in record dir, removes the attached file.
func (c *Cache) checkAttach() {
	c.Fmutex.Lock()
	defer c.Fmutex.Unlock()
	dir := path.Join(c.CacheDir, c.dathash(), "attach")
	err := util.EachFiles(dir, func(d os.FileInfo) error {
		idstr := d.Name()
		if i := strings.IndexRune(idstr, '.'); i > 0 {
			idstr = idstr[:i]
		}
		if strings.HasPrefix(idstr, "s") {
			idstr = idstr[1:]
		}
		rec := NewRecord(c.Datfile, idstr)
		if !util.IsFile(rec.path()) {
			err := os.Remove(path.Join(dir, d.Name()))
			if err != nil {
				log.Println(err)
			}
		}
		return nil
	})
	if err != nil {
		log.Println(err)
	}
}

//Remove Remove all files and dirs of cache.
func (c *Cache) Remove() {
	c.Fmutex.Lock()
	defer c.Fmutex.Unlock()
	err := os.RemoveAll(c.datpath())
	if err != nil {
		log.Println(err)
	}
}

//Exists return true is datapath exists.
func (c *Cache) Exists() bool {
	c.Fmutex.RLock()
	defer c.Fmutex.RUnlock()
	return util.IsDir(c.datpath())
}

//getWithRange gets records with range using node n and adds to cache after checking them.
//if no records exist in cache, uses head
//return true if gotten records>0
func (c *Cache) getWithRange(n *node.Node) bool {
	now := time.Now().Unix()

	begin := c.ReadInfo().Stamp
	begin2 := now - c.SyncRange
	if begin2 < begin {
		begin = begin2
	}

	if !c.HasRecord() {
		begin = now - c.GetRange
	}

	res, err := n.Talk(fmt.Sprintf("/get/%s/%d-", c.Datfile, begin))
	if err != nil {
		return false
	}
	count, err := c.checkData(res, -1, "", begin, now)
	if err == nil || count > 0 {
		log.Println(c.Datfile, count, "records were saved")
	}
	return count > 0
}

//GetCache checks  nodes in lookuptable have the cache.
//if found gets records.
func (c *Cache) GetCache() bool {
	n := c.NodeManager.Search(c.Datfile, nil)
	if n != nil {
		c.getWithRange(n)
		return true
	}
	return false
}

//GetData gets records from node n and checks its is same as stamp and id in args.
//save recs if success. returns errSpam or errGet.
func (c *Cache) GetData(stamp int64, id string, n *node.Node) error {
	res, err := n.Talk(fmt.Sprintf("/get/%s/%d/%s", c.Datfile, stamp, id))
	if err != nil {
		log.Println(err)
		return errGet
	}
	count, err := c.checkData(res, stamp, id, -1, -1)
	if count == 0 {
		log.Println(c.Datfile, stamp, "records not found")
	}
	return err
}

//Gettitle returns title part if *_*.
//returns ca.datfile if not.
func (c *Cache) Gettitle() string {
	if strings.HasPrefix(c.Datfile, "thread_") {
		return util.FileDecode(c.Datfile)
	}
	return c.Datfile
}

//GetContents returns recstrs of cache.
//len(recstrs) is <=2.
func (c *Cache) GetContents() []string {
	contents := make([]string, 0, 2)
	recs := c.LoadRecords()
	for _, rec := range recs {
		err := rec.Load()
		if err != nil {
			log.Println(err)
		}
		contents = append(contents, util.Escape(rec.Recstr()))
		if len(contents) > 2 {
			return contents
		}
	}
	return contents
}
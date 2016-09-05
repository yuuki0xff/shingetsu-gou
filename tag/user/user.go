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

package user

import (
	"log"

	"github.com/shingetsu-gou/shingetsu-gou/db"
	"github.com/shingetsu-gou/shingetsu-gou/tag"
)

//Get tags from the disk  if dirty and returns Slice.
func Get() tag.Slice {
	db.Mutex.RLock()
	defer db.Mutex.RUnlock()
	var r []string
	if _, err := db.Map.Select(&r, "select  Tag from usertag"); err != nil {
		log.Print(err)
		return nil
	}
	return tag.NewSlice(r)
}

//GetThread gets thread tags from the disk
func GetThread(thread string) tag.Slice {
	db.Mutex.RLock()
	defer db.Mutex.RUnlock()
	var r []string
	if _, err := db.Map.Select(&r, "select  Tag from usertag where thread=?", thread); err != nil {
		log.Print(err)
		return nil
	}
	return tag.NewSlice(r)
}

//Set saves usertag.
func Set(thread string, tag []string) {
	db.Mutex.Lock()
	defer db.Mutex.Unlock()
	r := db.UserTag{
		Thread: thread,
	}
	for _, t := range tag {
		r.Tag = t
		if err := db.Map.Insert(&r); err != nil {
			log.Print(err)
		}
	}
}

//Set saves usertag.
func SetTags(thread string, tag tag.Slice) {
	db.Mutex.Lock()
	defer db.Mutex.Unlock()
	r := db.UserTag{
		Thread: thread,
	}
	for _, t := range tag {
		r.Tag = t.Tagstr
		if err := db.Map.Insert(&r); err != nil {
			log.Print(err)
		}
	}
}
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

package manager

import (
	"log"
	"math/rand"
	"strconv"
	"strings"
	"sync"

	"github.com/shingetsu-gou/shingetsu-gou/db"
	"github.com/shingetsu-gou/shingetsu-gou/node"
)

const (
	defaultNodes = 5 // Nodes keeping in node list
	shareNodes   = 5 // Nodes having the file
)

//Manager represents the map that maps datfile to it's source node list.

//getFromList returns one node  in the nodelist.
func getFromList() *node.Node {
	r, err := db.String("select  Addr from lookup where Thread=''")
	if err != nil {
		log.Print(err)
		return nil
	}

	n, err := node.New(r)
	if err != nil {
		log.Println(err)
		return nil
	}
	return n
}

//NodeLen returns size of all nodes.
func NodeLen() int {
	ns := getAllNodes()
	return ns.Len()
}

//ListLen returns size of nodelist.
func ListLen() int {
	return listLen("")
}

func listLen(datfile string) int {
	r, err := db.Int64("select count(*) from lookup where Thread=?", datfile)
	if err != nil {
		log.Print(err)
		return 0
	}

	return int(r)
}

//GetNodestrSlice returns Nodestr of all nodes.
func GetNodestrSlice() []string {
	return getAllNodes().GetNodestrSlice()
}

//getAllNodes returns all nodes in table.
func getAllNodes() node.Slice {
	r, err := db.Strings("select  distinct Addr from lookup group by Addr")
	if err != nil {
		log.Print(err)
		return nil
	}
	return node.NewSlice(r)
}

//Get returns rawnodelist associated with datfile
//if not found returns def
func Get(datfile string, def node.Slice) node.Slice {
	str := GetNodestrSliceInTable(datfile)
	if str == nil {
		return def
	}
	return node.NewSlice(str)
}

//GetNodestrSliceInTable returns Nodestr slice of nodes associated datfile thread.
func GetNodestrSliceInTable(datfile string) []string {
	r, err := db.Strings("select  Addr from lookup where Thread=?", datfile)
	if err != nil {
		log.Print(err)
		return nil
	}
	return r
}

//Random selects # of min(all # of nodes,n) nodes randomly except exclude nodes.
func Random(exclude node.Slice, num int) []*node.Node {
	all := getAllNodes()
	if exclude != nil {
		cand := make([]*node.Node, 0, len(all))
		m := exclude.ToMap()
		for _, n := range all {
			if _, exist := m[n.Nodestr]; !exist {
				cand = append(cand, n)
			}
		}
		all = cand
	}
	n := all.Len()
	if num < n && num != 0 {
		n = num
	}
	r := make([]*node.Node, n)
	rs := rand.Perm(all.Len())
	for i := 0; i < n; i++ {
		r[i] = all[rs[i]]
	}
	return r
}

//AppendToTable add node n to table if it is allowd and list doesn't have it.
func AppendToTable(datfile string, n *node.Node) {
	l := listLen(datfile)
	if ((datfile != "" && l < shareNodes) || (datfile == "" && l < defaultNodes)) &&
		n != nil && n.IsAllowed() && !hasNodeInTable(datfile, n) {
		_, err := db.DB.Exec("insert into lookup(Thread,Addr) values(?,?)", datfile, n.Nodestr)
		if err != nil {
			log.Println(err)
		}
	}
}

//appendToList add node n to nodelist if it is allowd and list doesn't have it.
func appendToList(n *node.Node) {
	AppendToTable("", n)
}

//ReplaceNodeInList removes one node and say bye to the node and add n in nodelist.
//if len(node)>defaultnode
func ReplaceNodeInList(n *node.Node) *node.Node {
	l := ListLen()
	if !n.IsAllowed() || hasNodeInTable("", n) {
		return nil
	}
	var old *node.Node
	if l >= defaultNodes {
		old = getFromList()
		RemoveFromList(old)
		old.Bye()
	}
	appendToList(n)
	return old
}

//hasNodeInTable returns true if nodelist has n.
func hasNodeInTable(datfile string, n *node.Node) bool {
	r, err := db.Int64("select  count(*) from lookup where Addr=? and Thread=?", n.Nodestr, datfile)
	if err != nil {
		log.Print(err)
		return false
	}
	return r != 0
}

//RemoveFromTable removes node n and return true if exists.
//or returns false if not exists.
func RemoveFromTable(datfile string, n *node.Node) bool {
	if n == nil {
		log.Println("n is nil")
		return false
	}
	if !hasNodeInTable(datfile, n) {
		return false
	}
	_, err := db.DB.Exec("delete from lookup where Thread=? and Addr=? ", datfile, n.Nodestr)
	if err != nil {
		log.Println(err)
		return false
	}
	return true
}

//RemoveFromList removes node n from nodelist and return true if exists.
//or returns false if not exists.
func RemoveFromList(n *node.Node) bool {
	return RemoveFromTable("", n)
}

//RemoveFromAllTable removes node n from all tables and return true if exists.
//or returns false if not exists.
func RemoveFromAllTable(n *node.Node) bool {
	_, err := db.DB.Exec("delete from lookup where  Addr=? ", n.Nodestr)
	if err != nil {
		log.Println(err)
		return false
	}
	return true
}

//Initialize pings one of initNode except myself and added it if success,
//and get another node info from each nodes in nodelist.
func Initialize(allnodes node.Slice) {
	inodes := allnodes
	if len(allnodes) > defaultNodes {
		inodes = inodes[:defaultNodes]
	}
	var wg sync.WaitGroup
	pingOK := make([]*node.Node, 0, len(inodes))
	var mutex sync.Mutex
	for i := 0; i < len(inodes) && ListLen() < defaultNodes; i++ {
		wg.Add(1)
		go func(inode *node.Node) {
			defer wg.Done()
			if err := inode.Ping(); err == nil {
				mutex.Lock()
				pingOK = append(pingOK, inode)
				mutex.Unlock()
				go Join(inode)
			}
		}(inodes[i])
	}
	wg.Wait()

	log.Println("# of nodelist:", ListLen())
	tx, err := db.DB.Begin()
	if err != nil {
		log.Print(err)
		return
	}
	for _, p := range pingOK {
		appendToList(p)
	}
	if err := tx.Commit(); err != nil {
		log.Println(err)
	}
}

//Join tells n to join and adds n to nodelist if welcomed.
//if n returns another nodes, repeats it and return true..
//removes fron nodelist if not welcomed and return false.
func Join(n *node.Node) bool {
	const retryJoin = 2 // Times; Join network
	if n == nil {
		return false
	}
	flag := false
	if hasNodeInTable("", n) || node.Me().Nodestr == n.Nodestr {
		return false
	}
	tx, err := db.DB.Begin()
	if err != nil {
		log.Print(err)
		return false
	}
	for count := 0; count < retryJoin && ListLen() < defaultNodes; count++ {
		extnode, err := n.Join()
		if err != nil {
			RemoveFromTable("", n)
			return flag
		}
		if extnode == nil {
			appendToList(n)
			return true
		}
		appendToList(n)
		n = extnode
		flag = true
	}
	if err := tx.Commit(); err != nil {
		log.Println(err)
	}
	return flag
}

//TellUpdate makes mynode info from node or dnsname or ip addr,
//and broadcast the updates of record id=id in cache c.datfile with stamp.
func TellUpdate(datfile string, stamp int64, id string, n *node.Node) {
	const updateNodes = 10

	tellstr := node.Me().Toxstring()
	if n != nil {
		tellstr = n.Toxstring()
	}
	msg := strings.Join([]string{"/update", datfile, strconv.FormatInt(stamp, 10), id, tellstr}, "/")

	ns := Get(datfile, nil)
	ns = ns.Extend(Get("", nil))
	ns = ns.Extend(Random(ns, updateNodes))
	log.Println("telling #", len(ns))
	for _, n := range ns {
		_, err := n.Talk(msg, nil)
		if err != nil {
			log.Println(err)
		}
	}
}

//NodesForGet returns nodes which has datfile cache , and that extends nodes to #searchDepth .
func NodesForGet(datfile string, searchDepth int) node.Slice {
	var ns, ns2 node.Slice
	ns = ns.Extend(Get(datfile, nil))
	ns = ns.Extend(Get("", nil))
	ns = ns.Extend(Random(ns, 0))

	for _, n := range ns {
		if !n.Equals(node.Me()) && n.IsAllowed() {
			ns2 = append(ns2, n)
		}
	}
	if ns2.Len() > searchDepth {
		ns2 = ns2[:searchDepth]
	}
	return ns2
}

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

package node

import (
	"log"
	"math/rand"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/shingetsu-gou/go-nat"
	"github.com/shingetsu-gou/shingetsu-gou/util"
)

const (
	defaultNodes = 5 // Nodes keeping in node list
	shareNodes   = 5 // Nodes having the file
)

//ManagerConfig contains params for NodeManager struct.
type ManagerConfig struct {
	ServerName  string
	Lookup      string
	DefaultPort int
	EnableNAT   bool
	ServerURL   string
	Fmutex      *sync.RWMutex
	NodeAllow   *util.RegexpList
	NodeDeny    *util.RegexpList
	Myself      *Node
	InitNode    *util.ConfList
}

//Manager represents the map that maps datfile to it's source node list.
type Manager struct {
	*ManagerConfig
	isDirty      bool
	nodes        map[string]nodeSlice //map[""] is nodelist
	externalPort int
	mutex        sync.RWMutex
}

//NewManager read the file and returns NodeManager obj.
func NewManager(cfg *ManagerConfig) *Manager {
	r := &Manager{
		ManagerConfig: cfg,
		nodes:         make(map[string]nodeSlice),
		externalPort:  cfg.DefaultPort,
	}
	err := util.EachKeyValueLine(cfg.Lookup, func(key string, value []string, i int) error {
		var nl nodeSlice
		for _, v := range value {
			nl = append(nl, NewNode(v))
		}
		r.nodes[key] = nl
		return nil
	})
	if err != nil {
		log.Println(err)
	}
	if r.EnableNAT {
		r.setUPnP()
	}
	return r
}

//setMyself makes node from nodestr and set to myself obj.
func (lt *Manager) setMyself(nodestr string) {
	lt.mutex.Lock()
	lt.Myself = MakeNode(nodestr, lt.ServerURL, lt.externalPort)
	lt.mutex.Unlock()
}

//GetMyself returns myself if servername is not set.
//or makes and returns a node from servername.
func (lt *Manager) GetMyself() *Node {
	if lt.ServerName != "" {
		return MakeNode(lt.ServerName, lt.ServerURL, lt.externalPort)
	}
	return lt.Myself
}

//setUPnP gets external port by upnp if enabled.
func (lt *Manager) setUPnP() {
	nt, err := nat.NewNetStatus()
	if err != nil {
		log.Println(err)
	} else {
		m, err := nt.LoopPortMapping("tcp", lt.DefaultPort, "shingetsu-gou", 10*time.Minute)
		if err != nil {
			log.Println(err)
		} else {
			lt.externalPort = m.ExternalPort
		}
	}
}

//getFromList returns number=n in the nodelist.
func (lt *Manager) getFromList(n int) *Node {
	lt.mutex.RLock()
	defer lt.mutex.RUnlock()
	if lt.ListLen() == 0 {
		return nil
	}
	return lt.nodes[""][n]
}

//NodeLen returns size of all nodes.
func (lt *Manager) NodeLen() int {
	ns := lt.getAllNodes()
	return ns.Len()
}

//ListLen returns size of nodelist.
func (lt *Manager) ListLen() int {
	lt.mutex.RLock()
	defer lt.mutex.RUnlock()
	return len(lt.nodes[""])
}

//GetNodestrSlice returns Nodestr of all nodes.
func (lt *Manager) GetNodestrSlice() []string {
	return lt.getAllNodes().getNodestrSlice()
}

//getAllNodes returns all nodes in table.
func (lt *Manager) getAllNodes() nodeSlice {
	var n nodeSlice
	n = make([]*Node, lt.NodeLen())
	i := 0
	lt.mutex.RLock()
	defer lt.mutex.RUnlock()
	for _, v := range lt.nodes {
		for _, node := range v {
			n[i] = node
			i++
		}
	}
	return n.uniq()
}

//GetNodestrSliceInTable returns Nodestr slice of nodes associated datfile thread.
func (lt *Manager) GetNodestrSliceInTable(datfile string) []string {
	lt.mutex.RLock()
	defer lt.mutex.RUnlock()
	n := lt.nodes["datfile"]
	return n.getNodestrSlice()
}

//Random selects #n nodes randomly except exclude nodes.
func (lt *Manager) Random(exclude nodeSlice, num int) []*Node {
	all := lt.getAllNodes()
	if exclude != nil {
		for i, n := range all {
			if exclude.has(n) {
				all, all[len(all)-1] = append(all[:i], all[i+1:]...), nil
			}
		}
	}
	r := make([]*Node, num)
	rs := rand.Perm(all.Len() - 1)
	for i := 0; i < num; i++ {
		r[i] = all[rs[i]]
	}
	return r
}

//AppendToTable add node n to table if it is allowd and list doesn't have it.
func (lt *Manager) AppendToTable(datfile string, n *Node) {
	lt.mutex.RLock()
	l := len(lt.nodes[datfile])
	lt.mutex.RUnlock()
	if ((datfile != "" && l < shareNodes) || (datfile == "" && l < defaultNodes)) &&
		n.IsAllowed() && !lt.hasNodeInTable(datfile, n) {
		lt.mutex.Lock()
		lt.isDirty = true
		lt.nodes[datfile] = append(lt.nodes[datfile], n)
		lt.mutex.Unlock()
	}
}

//extendTable adds slice of nodes with check.
func (lt *Manager) extendToTable(datfile string, ns []*Node) {
	if ns == nil {
		return
	}
	for _, n := range ns {
		lt.AppendToTable(datfile, n)
	}
}

//appendToList add node n to nodelist if it is allowd and list doesn't have it.
func (lt *Manager) appendToList(n *Node) {
	lt.AppendToTable("", n)
}

//ReplaceNodeInList removes one node and say bye to the node and add n in nodelist.
//if len(node)>defaultnode
func (lt *Manager) ReplaceNodeInList(n *Node) *Node {
	lt.mutex.RLock()
	l := len(lt.nodes[""])
	lt.mutex.RUnlock()
	if !n.IsAllowed() || lt.hasNodeInTable("", n) {
		return nil
	}
	var old *Node
	if l > defaultNodes {
		old := lt.getFromList(0)
		lt.RemoveFromList(old)
		old.bye()
	}
	lt.appendToList(n)
	return old
}

//extendToList adds node slice to nodelist.
func (lt *Manager) extendToList(ns []*Node) {
	lt.extendToTable("", ns)
}

//hasNode returns true if nodelist in all tables has n.
func (lt *Manager) hasNode(n *Node) bool {
	return len(lt.findNode(n)) > 0
}

//findNode returns datfile of node n, or -1 if not exist.
func (lt *Manager) findNode(n *Node) []string {
	lt.mutex.RLock()
	defer lt.mutex.RUnlock()
	var r []string
	for k := range lt.nodes {
		if lt.hasNodeInTable(k, n) {
			r = append(r, k)
		}
	}
	return r
}

//hasNodeInTable returns true if nodelist has n.
func (lt *Manager) hasNodeInTable(datfile string, n *Node) bool {
	return lt.findNodeInTable(datfile, n) != -1
}

//findNode returns location of node n, or -1 if not exist.
func (lt *Manager) findNodeInTable(datfile string, n *Node) int {
	return util.FindString(lt.GetNodestrSliceInTable(datfile), n.Nodestr)
}

//RemoveFromTable removes node n and return true if exists.
//or returns false if not exists.
func (lt *Manager) RemoveFromTable(datfile string, n *Node) bool {
	lt.mutex.Lock()
	defer lt.mutex.Unlock()
	if i := util.FindString(lt.nodes[datfile].getNodestrSlice(), n.Nodestr); i >= 0 {
		lt.nodes[datfile] = append(lt.nodes[datfile][:i], lt.nodes[datfile][i+1:]...)
		lt.isDirty = true
		return true
	}
	return false
}

//RemoveFromList removes node n from nodelist and return true if exists.
//or returns false if not exists.
func (lt *Manager) RemoveFromList(n *Node) bool {
	return lt.RemoveFromTable("", n)
}

//RemoveFromAllTable removes node n from all tables and return true if exists.
//or returns false if not exists.
func (lt *Manager) RemoveFromAllTable(n *Node) bool {
	del := false
	lt.mutex.RLock()
	for k := range lt.nodes {
		defer lt.mutex.RUnlock()
		del = del || lt.RemoveFromTable(k, n)
	}
	return del
}

//moreNodes gets another node info from each nodes in nodelist.
func (lt *Manager) moreNodes() {
	const retry = 5 // Times; Common setting

	no := 0
	count := 0
	all := lt.getAllNodes()
	for lt.NodeLen() < defaultNodes {
		nn := all[no]
		newN := nn.getNode()
		lt.Join(newN)
		if count++; count > retry {
			count = 0
			if no++; no >= len(all) {
				return
			}
		}
	}
}

//Initialize pings one of initNode except myself and added it if success,
//and get another node info from each nodes in nodelist.
func (lt *Manager) Initialize() {

	if lt.ListLen() > defaultNodes {
		return
	}
	for _, i := range lt.InitNode.GetData() {
		inode := NewNode(i)
		if _, err := inode.Ping(); err == nil {
			lt.Join(inode)
			break
		}
	}
	if lt.Myself != nil {
		lt.RemoveFromAllTable(lt.Myself)
	}
	if lt.NodeLen() > 0 {
		lt.moreNodes()
	}
	if lt.NodeLen() <= 1 {
		log.Println("few linked nodes")
	}
	lt.Sync()
}

//Join tells n to join and adds n to nodelist if welcomed.
//if n returns another nodes, repeats it and return true..
//removes fron nodelist if not welcomed and return false.
func (lt *Manager) Join(n *Node) bool {
	const retryJoin = 2 // Times; Join network

	if n == nil {
		return false
	}
	flag := false
	if lt.hasNode(n) {
		return false
	}
	for count := 0; count < retryJoin && lt.NodeLen() < defaultNodes; count++ {
		welcome, extnode := n.join()
		if welcome && extnode == nil {
			lt.appendToList(n)
			return true
		}
		if welcome {
			lt.appendToList(n)
			n = extnode
			flag = true
		} else {
			lt.RemoveFromTable("", n)
			return flag
		}
	}
	return flag
}

//TellUpdate makes mynode info from node or dnsname or ip addr,
//and broadcast the updates of record id=id in cache c.datfile with stamp.
func (lt *Manager) TellUpdate(datfile string, stamp int64, id string, node *Node) {
	var tellstr string
	switch {
	case node != nil:
		tellstr = node.toxstring()
	case lt.ServerName != "":
		tellstr = lt.Myself.toxstring()
	default:
		tellstr = ":" + strconv.Itoa(lt.externalPort) + strings.Replace(lt.ServerURL, "/", "+", -1)
	}
	msg := strings.Join([]string{"/update", datfile, strconv.FormatInt(stamp, 10), id, tellstr}, "/")

	ns := lt.get(datfile, nil)
	ns.extend(lt.get("", nil))

	for _, n := range ns {
		_, err := n.Talk(msg)
		if err != nil {
			log.Println(err)
		}
	}
}

//get returns rawnodelist associated with datfile
//if not found returns def
func (lt *Manager) get(datfile string, def nodeSlice) nodeSlice {
	lt.mutex.RLock()
	defer lt.mutex.RUnlock()
	if v, exist := lt.nodes[datfile]; exist {
		nodes := make([]*Node, v.Len())
		copy(nodes, v)
		return nodes
	}
	return nodeSlice(def)
}

//stringMap returns map of k=datfile, v=Nodestr of rawnodelist.
func (lt *Manager) stringMap() map[string][]string {
	lt.mutex.RLock()
	defer lt.mutex.RUnlock()
	result := make(map[string][]string)
	for k, v := range lt.nodes {
		if k == "" {
			continue
		}
		result[k] = v.getNodestrSlice()
	}
	return result
}

//Sync saves  k=datfile, v=Nodestr map to the file.
func (lt *Manager) Sync() {
	if lt.isDirty {
		m := lt.stringMap()
		lt.Fmutex.Lock()
		defer lt.Fmutex.Unlock()
		err := util.WriteMap(lt.Lookup, m)
		if err != nil {
			log.Println(err)
		} else {
			lt.mutex.Lock()
			lt.isDirty = false
			lt.mutex.Unlock()
		}
	}
}

//Search checks one allowed nodes which selected randomly from nodes has the datfile record.
//if not found,n is removed from lookuptable. also if not pingable  removes n from searchlist and cache c.
//if found, n is added to lookuptable.
func (lt *Manager) Search(datfile string, nodes []*Node) *Node {
	const searchDepth = 30 // Search node size

	ns := lt.get(datfile, nil)
	ns.extend(lt.get("", nil))
	ns.extend(nodes)
	if ns.Len() < searchDepth {
		ns = ns.extend(lt.Random(ns, searchDepth-ns.Len()))
	}
	count := 0
	for _, n := range ns {
		if n.equals(lt.Myself) || !n.IsAllowed() {
			continue
		}
		res, err := n.Talk("/have/" + datfile)
		if err == nil && len(res) > 0 && res[0] == "YES" {
			lt.AppendToTable(datfile, n)
			lt.Sync()
			return n
		}
		lt.RemoveFromTable(datfile, n)
		if count++; count > searchDepth {
			break
		}
	}
	if count <= 1 {
		log.Println("Warning: Search nodes are null.")
	}
	return nil
}

//Rejoin adds nodes in searchlist if ping is ok and len(nodelist)<defaultNodes
//and doesn't have it's node.
//if ping is ng, removes node from searchlist.
func (lt *Manager) Rejoin() {
	all := lt.getAllNodes()
	for _, n := range all {
		if lt.ListLen() >= defaultNodes {
			return
		}
		lt.mutex.RLock()
		has := lt.nodes[""].has(n)
		lt.mutex.RUnlock()
		if has {
			continue
		}
		if _, err := n.Ping(); err == nil || !lt.Join(n) {
			lt.RemoveFromAllTable(n)
			lt.Sync()
		} else {
			lt.appendToList(n)
		}
	}
	if lt.ListLen() <= 1 {
		log.Println("Warning: Few linked nodes")
	}
}

//PingAll pings to all nodes in nodelist.
//if ng, removes from nodelist.
func (lt *Manager) PingAll() {
	lt.mutex.RLock()
	for _, n := range lt.nodes[""] {
		lt.mutex.RUnlock()
		if _, err := n.Ping(); err != nil {
			lt.RemoveFromAllTable(n)
		}
		lt.mutex.RLock()
	}
	lt.mutex.RUnlock()
}

//RejoinList joins all node in nodelist.
func (lt *Manager) RejoinList() {
	lt.mutex.RLock()
	defer lt.mutex.RUnlock()
	for _, n := range lt.nodes[""] {
		n.join()
	}
}
// Copyright (C) 2026  mieru authors
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package protocol

import (
	"encoding/hex"
	"net"
	"sort"
	"sync"
	"time"

	"github.com/enfein/mieru/v3/pkg/appctl/appctlpb"
	"github.com/enfein/mieru/v3/pkg/cipher"
	"github.com/enfein/mieru/v3/pkg/log"
)

const (
	serverAuthCacheMaxEntries = 65536
	serverAuthCacheMRUSize    = 4
	serverAuthCacheTTL        = 10 * time.Minute
)

type serverAuthUser struct {
	name      string
	nameBytes []byte
	password  []byte
}

type serverAuthTable struct {
	users      []*serverAuthUser
	byName     map[string]*serverAuthUser
	generation uint64
}

func newServerAuthTable(users map[string]*appctlpb.User, generation uint64) *serverAuthTable {
	table := &serverAuthTable{
		users:      make([]*serverAuthUser, 0, len(users)),
		byName:     make(map[string]*serverAuthUser, len(users)),
		generation: generation,
	}
	names := make([]string, 0, len(users))
	for name := range users {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		user := users[name]
		authUser, ok := newServerAuthUser(user)
		if !ok {
			continue
		}
		table.users = append(table.users, authUser)
		table.byName[authUser.name] = authUser
	}
	return table
}

func newServerAuthUser(user *appctlpb.User) (*serverAuthUser, bool) {
	if user == nil || user.GetName() == "" {
		return nil, false
	}
	var password []byte
	if hashedPassword := user.GetHashedPassword(); hashedPassword != "" {
		decoded, err := hex.DecodeString(hashedPassword)
		if err != nil {
			log.Debugf("Unable to decode hashed password %q from user %q", hashedPassword, user.GetName())
			return nil, false
		}
		password = decoded
	} else {
		password = cipher.HashPassword([]byte(user.GetPassword()), []byte(user.GetName()))
	}
	return &serverAuthUser{
		name:      user.GetName(),
		nameBytes: []byte(user.GetName()),
		password:  password,
	}, true
}

func (t *serverAuthTable) lookup(name string) *serverAuthUser {
	if t == nil {
		return nil
	}
	return t.byName[name]
}

type serverAuthCacheEntry struct {
	users   []string
	updated time.Time
}

type serverAuthCacheMapKey struct {
	generation uint64
	sourceIP   string
}

type serverAuthCache struct {
	mu      sync.Mutex
	entries map[serverAuthCacheMapKey]*serverAuthCacheEntry
}

func newServerAuthCache() *serverAuthCache {
	return &serverAuthCache{
		entries: make(map[serverAuthCacheMapKey]*serverAuthCacheEntry),
	}
}

func (c *serverAuthCache) candidates(remoteAddr net.Addr, table *serverAuthTable) []*serverAuthUser {
	if c == nil || table == nil {
		return nil
	}
	key, ok := serverAuthCacheKey(remoteAddr, table.generation)
	if !ok {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[key]
	if !ok {
		return nil
	}
	now := time.Now()
	if now.Sub(entry.updated) > serverAuthCacheTTL {
		delete(c.entries, key)
		return nil
	}
	candidates := make([]*serverAuthUser, 0, len(entry.users))
	for _, name := range entry.users {
		if user := table.lookup(name); user != nil {
			candidates = append(candidates, user)
		}
	}
	if len(candidates) == 0 {
		delete(c.entries, key)
		return nil
	}
	return candidates
}

func (c *serverAuthCache) record(remoteAddr net.Addr, table *serverAuthTable, userName string) {
	if c == nil || table == nil || userName == "" || table.lookup(userName) == nil {
		return
	}
	key, ok := serverAuthCacheKey(remoteAddr, table.generation)
	if !ok {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	entry, ok := c.entries[key]
	if !ok || now.Sub(entry.updated) > serverAuthCacheTTL {
		entry = &serverAuthCacheEntry{
			users: make([]string, 0, serverAuthCacheMRUSize),
		}
		c.entries[key] = entry
	}
	for index, cachedUser := range entry.users {
		if cachedUser == userName {
			copy(entry.users[1:index+1], entry.users[:index])
			entry.users[0] = userName
			entry.updated = now
			return
		}
	}
	if len(entry.users) < serverAuthCacheMRUSize {
		entry.users = append(entry.users, "")
	}
	copy(entry.users[1:], entry.users[:len(entry.users)-1])
	entry.users[0] = userName
	if len(entry.users) > serverAuthCacheMRUSize {
		entry.users = entry.users[:serverAuthCacheMRUSize]
	}
	entry.updated = now
	if len(c.entries) > serverAuthCacheMaxEntries {
		c.prune(now)
	}
}

func (c *serverAuthCache) prune(now time.Time) {
	var oldestKey serverAuthCacheMapKey
	var oldest time.Time
	hasOldest := false
	for key, entry := range c.entries {
		if now.Sub(entry.updated) > serverAuthCacheTTL {
			delete(c.entries, key)
			continue
		}
		if !hasOldest || entry.updated.Before(oldest) {
			oldestKey = key
			oldest = entry.updated
			hasOldest = true
		}
	}
	if len(c.entries) > serverAuthCacheMaxEntries && hasOldest {
		delete(c.entries, oldestKey)
	}
}

func serverAuthCacheKey(remoteAddr net.Addr, generation uint64) (serverAuthCacheMapKey, bool) {
	if remoteAddr == nil {
		return serverAuthCacheMapKey{}, false
	}
	var sourceIP string
	switch addr := remoteAddr.(type) {
	case *net.TCPAddr:
		sourceIP = addr.IP.String()
	case *net.UDPAddr:
		sourceIP = addr.IP.String()
	default:
		host, _, err := net.SplitHostPort(remoteAddr.String())
		if err == nil {
			sourceIP = host
		} else {
			sourceIP = remoteAddr.String()
		}
	}
	if sourceIP == "" {
		return serverAuthCacheMapKey{}, false
	}
	return serverAuthCacheMapKey{
		generation: generation,
		sourceIP:   sourceIP,
	}, true
}

func serverTryDecryptAuthUser(encryptedMeta, nonce []byte, authUser *serverAuthUser, stateless bool, requireHint bool, recordHintMetric bool) (cipher.BlockCipher, []byte, bool) {
	if authUser == nil {
		return nil, nil, false
	}
	if requireHint && !cipher.CheckUserFromHint(authUser.nameBytes, nonce) {
		return nil, nil, false
	}
	if recordHintMetric {
		cipher.ServerHintMatchDecrypt.Add(1)
	}
	block, decrypted, err := cipher.TryDecrypt(encryptedMeta, authUser.password, stateless)
	if err != nil {
		if recordHintMetric {
			cipher.ServerFailedHintMatchDecrypt.Add(1)
		}
		return nil, nil, false
	}
	return block, decrypted, true
}

func serverAuthUserWasTried(name string, tried []string) bool {
	for _, triedName := range tried {
		if triedName == name {
			return true
		}
	}
	return false
}

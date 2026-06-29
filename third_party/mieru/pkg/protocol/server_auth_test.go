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
	"net"
	"testing"

	"github.com/enfein/mieru/v3/pkg/appctl/appctlpb"
	"github.com/enfein/mieru/v3/pkg/cipher"
	"github.com/enfein/mieru/v3/pkg/metrics"
	"google.golang.org/protobuf/proto"
)

func TestServerAuthCacheUsesSourceIPMRU(t *testing.T) {
	table := newServerAuthTable(map[string]*appctlpb.User{
		"alice": {Name: proto.String("alice"), Password: proto.String("alice-password")},
		"bob":   {Name: proto.String("bob"), Password: proto.String("bob-password")},
	}, 1)
	cache := newServerAuthCache()
	firstAddr := &net.TCPAddr{IP: net.ParseIP("203.0.113.10"), Port: 10001}
	secondAddr := &net.TCPAddr{IP: net.ParseIP("203.0.113.10"), Port: 10002}

	cache.record(firstAddr, table, "alice")
	cache.record(secondAddr, table, "bob")

	candidates := cache.candidates(firstAddr, table)
	if len(candidates) != 2 {
		t.Fatalf("len(candidates) = %d, want 2", len(candidates))
	}
	if candidates[0].name != "bob" || candidates[1].name != "alice" {
		t.Fatalf("candidate order = [%s, %s], want [bob, alice]", candidates[0].name, candidates[1].name)
	}
}

func TestServerAuthCacheGenerationMismatchMisses(t *testing.T) {
	oldTable := newServerAuthTable(map[string]*appctlpb.User{
		"alice": {Name: proto.String("alice"), Password: proto.String("alice-password")},
	}, 1)
	newTable := newServerAuthTable(map[string]*appctlpb.User{
		"alice": {Name: proto.String("alice"), Password: proto.String("new-password")},
	}, 2)
	cache := newServerAuthCache()
	addr := &net.UDPAddr{IP: net.ParseIP("203.0.113.20"), Port: 10001}

	cache.record(addr, oldTable, "alice")
	if got := cache.candidates(addr, newTable); len(got) != 0 {
		t.Fatalf("len(candidates) = %d, want 0", len(got))
	}
}

func TestServerTryDecryptAuthUserRequiresMatchingHint(t *testing.T) {
	table := newServerAuthTable(map[string]*appctlpb.User{
		"alice": {Name: proto.String("alice"), Password: proto.String("alice-password")},
		"bob":   {Name: proto.String("bob"), Password: proto.String("bob-password")},
	}, 1)
	authUser := table.lookup("bob")
	block, err := cipher.BlockCipherFromPassword(authUser.password, true)
	if err != nil {
		t.Fatalf("BlockCipherFromPassword() failed: %v", err)
	}
	block.SetBlockContext(cipher.BlockContext{UserName: authUser.name})
	encryptedMeta, err := block.Encrypt(make([]byte, MetadataLength))
	if err != nil {
		t.Fatalf("Encrypt() failed: %v", err)
	}
	nonce := encryptedMeta[:cipher.DefaultNonceSize]

	if _, _, ok := serverTryDecryptAuthUser(encryptedMeta, nonce, table.lookup("alice"), true, true, false); ok {
		t.Fatal("serverTryDecryptAuthUser() with wrong user succeeded")
	}
	if _, decrypted, ok := serverTryDecryptAuthUser(encryptedMeta, nonce, authUser, true, true, false); !ok {
		t.Fatal("serverTryDecryptAuthUser() with matching user failed")
	} else if len(decrypted) != MetadataLength {
		t.Fatalf("decrypted len = %d, want %d", len(decrypted), MetadataLength)
	}
}

func TestSessionUserMetricType(t *testing.T) {
	session := &Session{
		users: map[string]*appctlpb.User{
			"alice": {
				Name:     proto.String("alice"),
				Password: proto.String("alice-password"),
			},
			"bob": {
				Name:     proto.String("bob"),
				Password: proto.String("bob-password"),
				Quotas: []*appctlpb.Quota{{
					Days:      proto.Int32(1),
					Megabytes: proto.Int32(1),
				}},
			},
		},
	}

	if got := session.userMetricType("alice"); got != metrics.COUNTER {
		t.Fatalf("userMetricType(alice) = %v, want COUNTER", got)
	}
	if got := session.userMetricType("bob"); got != metrics.COUNTER_TIME_SERIES {
		t.Fatalf("userMetricType(bob) = %v, want COUNTER_TIME_SERIES", got)
	}
}

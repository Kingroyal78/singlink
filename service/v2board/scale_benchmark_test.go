package v2board

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/singlink/singlink/option"
)

const (
	benchmarkTotalUsers   = 100_000
	benchmarkOnlineUsers  = 10_000
	benchmarkSSCipher     = "aes-128-gcm"
	benchmarkSS2022Cipher = "2022-blake3-aes-128-gcm"
)

func BenchmarkV2BoardSSScale100kDecodeUsers(b *testing.B) {
	users := benchmarkV2BoardSSUsers(benchmarkTotalUsers)
	body := benchmarkV2BoardSSUserListJSON(b, users)
	b.ReportAllocs()
	b.SetBytes(int64(len(body)))
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		userList, err := decodeUserList(body, "application/json")
		if err != nil {
			b.Fatal(err)
		}
		if len(userList.Users) != benchmarkTotalUsers {
			b.Fatalf("decoded %d users, expected %d", len(userList.Users), benchmarkTotalUsers)
		}
	}
}

func BenchmarkV2BoardSSScale100kMapInbound(b *testing.B) {
	users := benchmarkV2BoardSSUsers(benchmarkTotalUsers)
	config := benchmarkV2BoardSSConfig()
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		inbound, err := MapServerConfigInbound(config, users, MapperOptions{Tag: "benchmark-ss"})
		if err != nil {
			b.Fatal(err)
		}
		options, ok := inbound.Options.(*option.ShadowsocksInboundOptions)
		if !ok {
			b.Fatalf("unexpected inbound options %T", inbound.Options)
		}
		if len(options.Users) != benchmarkTotalUsers {
			b.Fatalf("mapped %d users, expected %d", len(options.Users), benchmarkTotalUsers)
		}
	}
}

func BenchmarkV2BoardSSScale100kTrackerInitialUpdate(b *testing.B) {
	users := benchmarkV2BoardSSUsers(benchmarkTotalUsers)
	now := time.Now()
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		state := &nodeTraffic{}
		state.updateUsersWithAliveState(1, users, nil, now, time.Minute, nodeRules{})
		if len(state.users) != benchmarkTotalUsers {
			b.Fatalf("tracked %d users, expected %d", len(state.users), benchmarkTotalUsers)
		}
	}
}

func BenchmarkV2BoardSSScale100kTrackerRefresh(b *testing.B) {
	users := benchmarkV2BoardSSUsers(benchmarkTotalUsers)
	state := &nodeTraffic{}
	state.updateUsersWithAliveState(1, users, nil, time.Now(), time.Minute, nodeRules{})
	now := time.Now()
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		state.updateUsersWithAliveState(1, users, nil, now, time.Minute, nodeRules{})
		if len(state.users) != benchmarkTotalUsers {
			b.Fatalf("tracked %d users, expected %d", len(state.users), benchmarkTotalUsers)
		}
	}
}

func BenchmarkV2BoardSSScale100kUsers10kOnlineSnapshot(b *testing.B) {
	users := benchmarkV2BoardSSUsers(benchmarkTotalUsers)
	onlineUsers := users[:benchmarkOnlineUsers]
	ips := benchmarkV2BoardSSIPs(benchmarkOnlineUsers)
	state := &nodeTraffic{}
	state.updateUsersWithAliveState(1, users, nil, time.Now(), time.Minute, nodeRules{})
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		for index, user := range onlineUsers {
			counter := state.counter(user.UUID)
			if counter == nil {
				b.Fatalf("missing counter for user %d", user.ID)
			}
			counter.upload.Add(512)
			counter.download.Add(512)
			if !state.markOnline(user.UUID, ips[index]) {
				b.Fatalf("mark online rejected user %d", user.ID)
			}
		}
		traffic, alive := state.snapshot(0, 0)
		if len(traffic) != benchmarkOnlineUsers {
			b.Fatalf("reported %d traffic users, expected %d", len(traffic), benchmarkOnlineUsers)
		}
		if len(alive) != benchmarkOnlineUsers {
			b.Fatalf("reported %d online users, expected %d", len(alive), benchmarkOnlineUsers)
		}
	}
}

func benchmarkV2BoardSSUsers(count int) []UserInfo {
	users := make([]UserInfo, count)
	for i := range users {
		id := i + 1
		users[i] = UserInfo{
			ID:     id,
			UUID:   fmt.Sprintf("00000000-0000-0000-0000-%012d", id),
			Cipher: benchmarkSSCipher,
			Secret: benchmarkBase64Key(fmt.Sprintf("benchmark-user-%d", id), 16),
		}
	}
	return users
}

func benchmarkV2BoardSSUserListJSON(b *testing.B, users []UserInfo) []byte {
	b.Helper()
	body, err := json.Marshal(UserListBody{Users: users})
	if err != nil {
		b.Fatal(err)
	}
	return body
}

func benchmarkV2BoardSSConfig() *ServerConfig {
	return &ServerConfig{
		Protocol:   "ss",
		ListenIP:   "127.0.0.1",
		ServerPort: 8388,
		Network:    "tcp",
		Cipher:     benchmarkSS2022Cipher,
		ServerKey:  benchmarkBase64Key("benchmark-server", 16),
	}
}

func benchmarkV2BoardSSIPs(count int) []string {
	ips := make([]string, count)
	for i := range ips {
		ips[i] = fmt.Sprintf("198.18.%d.%d", (i/250)%256, (i%250)+1)
	}
	return ips
}

func benchmarkBase64Key(seed string, length int) string {
	sum := sha256.Sum256([]byte(seed))
	return base64.StdEncoding.EncodeToString(sum[:length])
}

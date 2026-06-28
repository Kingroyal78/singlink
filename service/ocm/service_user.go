package ocm

import (
	"strings"
	"sync"

	E "github.com/sagernet/sing/common/exceptions"
	"github.com/singlink/singlink/option"
)

type UserManager struct {
	accessMutex sync.RWMutex
	tokenMap    map[string]string
}

func (m *UserManager) UpdateUsers(users []option.OCMUser) {
	m.accessMutex.Lock()
	defer m.accessMutex.Unlock()
	tokenMap := make(map[string]string, len(users))
	for _, user := range users {
		tokenMap[user.Token] = user.Name
	}
	m.tokenMap = tokenMap
}

func (m *UserManager) Authenticate(token string) (string, bool) {
	m.accessMutex.RLock()
	username, found := m.tokenMap[token]
	m.accessMutex.RUnlock()
	return username, found
}

func validateUsers(users []option.OCMUser) error {
	seen := make(map[string]int, len(users))
	for index, user := range users {
		if strings.TrimSpace(user.Token) == "" {
			return E.New("user[", index, "].token is required")
		}
		if previous, loaded := seen[user.Token]; loaded {
			return E.New("user[", index, "].token duplicates user[", previous, "].token")
		}
		seen[user.Token] = index
	}
	return nil
}

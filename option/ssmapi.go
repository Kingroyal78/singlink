package option

import (
	"github.com/sagernet/sing/common/json/badjson"
)

type SSMAPIServiceOptions struct {
	ListenOptions
	Servers   *badjson.TypedMap[string, string] `json:"servers"`
	Secret    string                            `json:"secret,omitempty"`
	CachePath string                            `json:"cache_path,omitempty"`
	InboundTLSOptionsContainer
}

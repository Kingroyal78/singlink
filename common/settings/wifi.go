package settings

import "github.com/singlink/singlink/adapter"

type WIFIMonitor interface {
	ReadWIFIState() adapter.WIFIState
	Start() error
	Close() error
}

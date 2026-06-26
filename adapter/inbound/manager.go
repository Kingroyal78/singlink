package inbound

import (
	"context"
	"os"
	"sync"

	"github.com/sagernet/sing/common"
	E "github.com/sagernet/sing/common/exceptions"
	"github.com/singlink/singlink/adapter"
	"github.com/singlink/singlink/common/taskmonitor"
	C "github.com/singlink/singlink/constant"
	"github.com/singlink/singlink/log"
)

var _ adapter.InboundManager = (*Manager)(nil)

type Manager struct {
	logger       log.ContextLogger
	registry     adapter.InboundRegistry
	endpoint     adapter.EndpointManager
	access       sync.Mutex
	lifecycle    sync.Mutex
	started      bool
	stage        adapter.StartStage
	inbounds     []adapter.Inbound
	inboundByTag map[string]adapter.Inbound
}

func NewManager(logger log.ContextLogger, registry adapter.InboundRegistry, endpoint adapter.EndpointManager) *Manager {
	return &Manager{
		logger:       logger,
		registry:     registry,
		endpoint:     endpoint,
		inboundByTag: make(map[string]adapter.Inbound),
	}
}

func (m *Manager) Start(stage adapter.StartStage) error {
	m.lifecycle.Lock()
	defer m.lifecycle.Unlock()
	m.access.Lock()
	if m.started && m.stage >= stage {
		panic("already started")
	}
	m.started = true
	m.stage = stage
	inbounds := m.inbounds
	m.access.Unlock()
	for _, inbound := range inbounds {
		name := "inbound/" + inbound.Type() + "[" + inbound.Tag() + "]"
		done := adapter.LogElapsed(m.logger, stage, " ", name)
		err := adapter.LegacyStart(inbound, stage)
		done()
		if err != nil {
			return E.Cause(err, stage, " ", name)
		}
	}
	return nil
}

func (m *Manager) Close() error {
	m.lifecycle.Lock()
	defer m.lifecycle.Unlock()
	m.access.Lock()
	if !m.started {
		m.access.Unlock()
		return nil
	}
	m.started = false
	inbounds := m.inbounds
	m.inbounds = nil
	m.access.Unlock()
	monitor := taskmonitor.New(m.logger, C.StopTimeout)
	var err error
	for _, inbound := range inbounds {
		name := "inbound/" + inbound.Type() + "[" + inbound.Tag() + "]"
		done := adapter.LogElapsed(m.logger, "close ", name)
		monitor.Start("close ", name)
		err = E.Append(err, inbound.Close(), func(err error) error {
			return E.Cause(err, "close ", name)
		})
		monitor.Finish()
		done()
	}
	return err
}

func (m *Manager) Inbounds() []adapter.Inbound {
	m.access.Lock()
	defer m.access.Unlock()
	return m.inbounds
}

func (m *Manager) Get(tag string) (adapter.Inbound, bool) {
	m.access.Lock()
	inbound, found := m.inboundByTag[tag]
	m.access.Unlock()
	if found {
		return inbound, true
	}
	return m.endpoint.Get(tag)
}

func (m *Manager) Remove(tag string) error {
	m.lifecycle.Lock()
	defer m.lifecycle.Unlock()
	m.access.Lock()
	inbound, found := m.inboundByTag[tag]
	if !found {
		m.access.Unlock()
		return os.ErrInvalid
	}
	delete(m.inboundByTag, tag)
	index := common.Index(m.inbounds, func(it adapter.Inbound) bool {
		return it == inbound
	})
	if index == -1 {
		panic("invalid inbound index")
	}
	m.inbounds = append(m.inbounds[:index], m.inbounds[index+1:]...)
	started := m.started
	m.access.Unlock()
	if started {
		return inbound.Close()
	}
	return nil
}

func (m *Manager) Create(ctx context.Context, router adapter.Router, logger log.ContextLogger, tag string, outboundType string, options any) error {
	inbound, err := m.registry.Create(ctx, router, logger, tag, outboundType, options)
	if err != nil {
		return err
	}
	m.lifecycle.Lock()
	defer m.lifecycle.Unlock()

	m.access.Lock()
	started := m.started
	existsInbound, exists := m.inboundByTag[tag]
	m.access.Unlock()

	if started {
		name := "inbound/" + inbound.Type() + "[" + inbound.Tag() + "]"
		for _, stage := range adapter.ListStartStages {
			done := adapter.LogElapsed(m.logger, stage, " ", name)
			err = adapter.LegacyStart(inbound, stage)
			done()
			if err != nil {
				startErr := E.Cause(err, stage, " ", name)
				closeErr := inbound.Close()
				if closeErr != nil {
					return E.Errors(startErr, E.Cause(closeErr, "close failed ", name))
				}
				return startErr
			}
		}
	}
	if exists && started {
		err = existsInbound.Close()
		if err != nil {
			closeErr := inbound.Close()
			if closeErr != nil {
				return E.Errors(
					E.Cause(err, "close inbound/", existsInbound.Type(), "[", existsInbound.Tag(), "]"),
					E.Cause(closeErr, "close failed inbound/", inbound.Type(), "[", inbound.Tag(), "]"),
				)
			}
			return E.Cause(err, "close inbound/", existsInbound.Type(), "[", existsInbound.Tag(), "]")
		}
	}

	m.access.Lock()
	defer m.access.Unlock()
	if existsInbound, loaded := m.inboundByTag[tag]; loaded {
		existsIndex := common.Index(m.inbounds, func(it adapter.Inbound) bool {
			return it == existsInbound
		})
		if existsIndex == -1 {
			panic("invalid inbound index")
		}
		m.inbounds = append(m.inbounds[:existsIndex], m.inbounds[existsIndex+1:]...)
	}
	m.inbounds = append(m.inbounds, inbound)
	m.inboundByTag[tag] = inbound
	return nil
}

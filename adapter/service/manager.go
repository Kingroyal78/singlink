package service

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

var _ adapter.ServiceManager = (*Manager)(nil)

type Manager struct {
	logger       log.ContextLogger
	registry     adapter.ServiceRegistry
	access       sync.Mutex
	started      bool
	stage        adapter.StartStage
	services     []adapter.Service
	serviceByTag map[string]adapter.Service
}

func NewManager(logger log.ContextLogger, registry adapter.ServiceRegistry) *Manager {
	return &Manager{
		logger:       logger,
		registry:     registry,
		serviceByTag: make(map[string]adapter.Service),
	}
}

func (m *Manager) Start(stage adapter.StartStage) error {
	m.access.Lock()
	if m.started && m.stage >= stage {
		panic("already started")
	}
	m.started = true
	m.stage = stage
	services := m.services
	m.access.Unlock()
	for _, service := range services {
		name := "service/" + service.Type() + "[" + service.Tag() + "]"
		done := adapter.LogElapsed(m.logger, stage, " ", name)
		err := adapter.LegacyStart(service, stage)
		done()
		if err != nil {
			return E.Cause(err, stage, " ", name)
		}
	}
	return nil
}

func (m *Manager) Close() error {
	m.access.Lock()
	defer m.access.Unlock()
	if !m.started {
		return nil
	}
	m.started = false
	services := m.services
	m.services = nil
	monitor := taskmonitor.New(m.logger, C.StopTimeout)
	var err error
	for _, service := range services {
		name := "service/" + service.Type() + "[" + service.Tag() + "]"
		done := adapter.LogElapsed(m.logger, "close ", name)
		monitor.Start("close ", name)
		err = E.Append(err, service.Close(), func(err error) error {
			return E.Cause(err, "close ", name)
		})
		monitor.Finish()
		done()
	}
	return err
}

func (m *Manager) Services() []adapter.Service {
	m.access.Lock()
	defer m.access.Unlock()
	return m.services
}

func (m *Manager) Get(tag string) (adapter.Service, bool) {
	m.access.Lock()
	service, found := m.serviceByTag[tag]
	m.access.Unlock()
	return service, found
}

func (m *Manager) Remove(tag string) error {
	m.access.Lock()
	service, found := m.serviceByTag[tag]
	if !found {
		m.access.Unlock()
		return os.ErrInvalid
	}
	delete(m.serviceByTag, tag)
	index := common.Index(m.services, func(it adapter.Service) bool {
		return it == service
	})
	if index == -1 {
		panic("invalid service index")
	}
	m.services = append(m.services[:index], m.services[index+1:]...)
	started := m.started
	m.access.Unlock()
	if started {
		return service.Close()
	}
	return nil
}

func (m *Manager) Create(ctx context.Context, logger log.ContextLogger, tag string, serviceType string, options any) error {
	service, err := m.registry.Create(ctx, logger, tag, serviceType, options)
	if err != nil {
		return err
	}
	m.access.Lock()
	defer m.access.Unlock()
	if m.started {
		name := "service/" + service.Type() + "[" + service.Tag() + "]"
		for _, stage := range adapter.ListStartStages {
			done := adapter.LogElapsed(m.logger, stage, " ", name)
			err = adapter.LegacyStart(service, stage)
			done()
			if err != nil {
				closeErr := service.Close()
				if closeErr != nil {
					return E.Errors(E.Cause(err, stage, " ", name), E.Cause(closeErr, "close failed ", name))
				}
				return E.Cause(err, stage, " ", name)
			}
		}
	}
	if existsService, loaded := m.serviceByTag[tag]; loaded {
		if m.started {
			err = existsService.Close()
			if err != nil {
				closeErr := service.Close()
				if closeErr != nil {
					return E.Errors(
						E.Cause(err, "close service/", existsService.Type(), "[", existsService.Tag(), "]"),
						E.Cause(closeErr, "close failed service/", service.Type(), "[", service.Tag(), "]"),
					)
				}
				return E.Cause(err, "close service/", existsService.Type(), "[", existsService.Tag(), "]")
			}
		}
		existsIndex := common.Index(m.services, func(it adapter.Service) bool {
			return it == existsService
		})
		if existsIndex == -1 {
			panic("invalid service index")
		}
		m.services = append(m.services[:existsIndex], m.services[existsIndex+1:]...)
	}
	m.services = append(m.services, service)
	m.serviceByTag[tag] = service
	return nil
}

package main

import (
	"errors"
	"sync"

	"pkg.deepin.io/lib/dbus1"
	"pkg.deepin.io/lib/dbusutil"
)

const (
	dbusPath        = "/com/deepin/ABRecovery"
	dbusInterface   = "com.deepin.ABRecovery"
	dbusServiceName = dbusInterface

	jobKindBackup  = "backup"
	jobKindRestore = "restore"
)

//go:generate dbusutil-gen -type Manager manager.go
type Manager struct {
	service       *dbusutil.Service
	PropsMu       sync.RWMutex
	BackingUp     bool
	Restoring     bool
	ConfigValid   bool
	BackupVersion string
	BackupTime    int64

	cfg Config

	methods *struct {
		CanBackup    func() `out:"can"`
		CanRestore   func() `out:"can"`
		StartBackup  func()
		StartRestore func()
	}

	signals *struct {
		JobEnd struct {
			kind    string
			success bool
			errMsg  string
		}
	}
}

func newManager(service *dbusutil.Service) *Manager {
	m := &Manager{
		service: service,
	}

	//var cfg Config
	err := loadConfig(configFile, &m.cfg)
	if err != nil {
		logger.Warning("failed to load config:", err)
	}
	logger.Debug("current:", m.cfg.Current)
	logger.Debug("backup:", m.cfg.Backup)

	err = m.cfg.check()
	if err != nil {
		logger.Warning(err)
	}
	m.ConfigValid = err == nil

	if m.ConfigValid {
		if m.cfg.Time != nil {
			m.BackupTime = m.cfg.Time.Unix()
		}
		m.BackupVersion = m.cfg.Version
	}

	return m
}

func (m *Manager) GetInterfaceName() string {
	return dbusInterface
}

func (m *Manager) canBackup() (bool, error) {
	if !m.ConfigValid {
		return false, nil
	}
	rootUuid, err := getRootUuid()
	if err != nil {
		return false, err
	}
	return rootUuid == m.cfg.Current, nil
}

func (m *Manager) CanBackup() (bool, *dbus.Error) {
	can, err := m.canBackup()
	return can, dbusutil.ToError(err)
}

func (m *Manager) canRestore() (bool, error) {
	if !m.ConfigValid {
		return false, nil
	}
	rootUuid, err := getRootUuid()
	if err != nil {
		return false, err
	}
	return rootUuid == m.cfg.Backup, nil
}

func (m *Manager) CanRestore() (bool, *dbus.Error) {
	can, err := m.canRestore()
	return can, dbusutil.ToError(err)
}

func (m *Manager) startBackup() error {
	can, err := m.canBackup()
	if err != nil {
		return err
	}

	if !can {
		return errors.New("backup cannot be performed")
	}

	m.PropsMu.Lock()
	if m.BackingUp {
		m.PropsMu.Unlock()
		return nil
	}

	m.BackingUp = true
	m.PropsMu.Unlock()
	err = m.emitPropChangedBackingUp(true)
	if err != nil {
		logger.Warning(err)
	}

	go func() {
		err := m.backup()
		if err != nil {
			logger.Warning(err)
		}
		m.emitSignalJobEnd(jobKindBackup, err)

		m.PropsMu.Lock()
		m.setPropBackingUp(false)
		if err == nil {
			backupTime := m.cfg.Time.Unix()
			m.setPropBackupTime(backupTime)
			m.setPropBackupVersion(m.cfg.Version)
		}
		m.PropsMu.Unlock()

	}()

	return nil
}

func (m *Manager) StartBackup() *dbus.Error {
	err := m.startBackup()
	return dbusutil.ToError(err)
}

func (m *Manager) startRestore() error {
	can, err := m.canRestore()
	if err != nil {
		return err
	}

	if !can {
		return errors.New("restore cannot be performed")
	}

	m.PropsMu.Lock()
	if m.Restoring {
		m.PropsMu.Unlock()
		return nil
	}

	m.Restoring = true
	m.PropsMu.Unlock()
	err = m.emitPropChangedRestoring(true)
	if err != nil {
		logger.Warning(err)
	}

	go func() {
		err := m.restore()
		if err != nil {
			logger.Warning(err)
		}
		m.emitSignalJobEnd(jobKindRestore, err)

		m.PropsMu.Lock()
		m.Restoring = false
		m.PropsMu.Unlock()

		err = m.emitPropChangedRestoring(false)
		if err != nil {
			logger.Warning(err)
		}
	}()

	return nil
}

func (m *Manager) StartRestore() *dbus.Error {
	err := m.startRestore()
	return dbusutil.ToError(err)
}

func (m *Manager) backup() error {
	return backup(&m.cfg)
}

func (m *Manager) restore() error {
	return restore(&m.cfg)
}

func (m *Manager) emitSignalJobEnd(kind string, err error) {
	switch kind {
	case jobKindBackup, jobKindRestore:
		// pass
	default:
		panic("invalid kind " + kind)
	}
	var errMsg string
	if err != nil {
		errMsg = err.Error()
	}
	success := err == nil
	emitErr := m.service.Emit(m, "JobEnd", kind, success, errMsg)
	if emitErr != nil {
		logger.Warning(err)
	}
}

func (m *Manager) canQuit() bool {
	m.PropsMu.Lock()
	can := !m.BackingUp && !m.Restoring
	m.PropsMu.Unlock()
	return can
}

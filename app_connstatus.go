package main

import (
	"github.com/ys-ll/uniterm/backend/connstatus"
	"github.com/ys-ll/uniterm/backend/session"
	"github.com/ys-ll/uniterm/backend/store"
)

// initConnStatus starts the start-screen reachability monitor (idle until
// the screen calls ConnStatusSetActive). It reads the stores at each round,
// so a data dir switch needs nothing here. See CONTRATO-status.md.
func (a *App) initConnStatus() {
	on := true
	if a.settingsStore != nil {
		if s, err := a.settingsStore.Load(); err == nil {
			on = s.ConnStatusCheckOn()
		}
	}
	a.connStatusOn.Store(on)
	if a.connStatus != nil {
		a.connStatus.Changed() // new data dir
		return
	}
	a.connStatus = connstatus.New(connstatus.Options{
		Connections: func() ([]session.ConnectionConfig, error) {
			if a.connectionStore == nil {
				return nil, nil
			}
			data, err := a.connectionStore.Load()
			return data.Connections, err
		},
		Proxies: func() map[string]bool {
			out := map[string]bool{}
			if a.proxyStore == nil {
				return out
			}
			data, err := a.proxyStore.Load()
			if err != nil {
				return nil // unknown ids count as proxies: nothing behind one is dialed
			}
			for _, p := range data.Proxies {
				out[p.ID] = p.IsActive()
			}
			return out
		},
		Enabled: a.connStatusOn.Load,
		Emit:    func(m map[string]connstatus.Status) { a.emit("connstatus:changed", m) },
	})
}

// connStatusNotify follows the events that change what is checked.
func (a *App) connStatusNotify(event string) {
	if a.connStatus != nil && (event == "store:connections:changed" || event == "store:proxies:changed") {
		a.connStatus.Changed()
	}
}

func (a *App) connStatusSettings(s store.AppSettings) {
	if a.connStatusOn.Swap(s.ConnStatusCheckOn()) != s.ConnStatusCheckOn() && a.connStatus != nil {
		a.connStatus.Changed()
	}
}

// ConnStatusGet returns the reachability of every saved connection.
func (a *App) ConnStatusGet() map[string]connstatus.Status {
	if a.connStatus == nil {
		return map[string]connstatus.Status{}
	}
	return a.connStatus.Get()
}

// ConnStatusSetActive: true while the start screen is visible and the
// window has focus; checks run only then.
func (a *App) ConnStatusSetActive(active bool) {
	if a.connStatus != nil {
		a.connStatus.SetActive(active)
	}
}

// ConnStatusCheckNow re-checks everything now (ignored within 10 s of the
// last round).
func (a *App) ConnStatusCheckNow() {
	if a.connStatus != nil {
		a.connStatus.CheckNow()
	}
}

// GetRecentAccess returns the recent connections with when each was last
// opened, most recent first.
func (a *App) GetRecentAccess() []store.RecentAccess {
	if a.recentStore == nil {
		return []store.RecentAccess{}
	}
	return a.recentStore.GetAccess()
}

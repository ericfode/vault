package vault

import (
	"log"
	"sync/atomic"
	"time"

	"github.com/hashicorp/vault/logical"
)

// RollbackManager is responsible for performing rollbacks of partial
// secrets within logical backends.
//
// During normal operations, it is possible for logical backends to
// error partially through an operation. These are called "partial secrets":
// they are never sent back to a user, but they do need to be cleaned up.
// This manager handles that by periodically (on a timer) requesting that the
// backends clean up.
//
// The RollbackManager periodically (according to the Period option)
// initiates a logical.RollbackOperation on every mounted logical backend.
// It ensures that only one rollback operation is in-flight at any given
// time within a single seal/unseal phase.
type RollbackManager struct {
	Logger *log.Logger
	Mounts *MountTable
	Router *Router

	Period time.Duration // time between rollback calls

	running uint32
}

// Start starts the rollback manager. This will block until Stop is called
// so it should be executed within a goroutine.
func (m *RollbackManager) Start() {
	// If we're already running, then don't start again
	if !atomic.CompareAndSwapUint32(&m.running, 0, 1) {
		return
	}

	m.Logger.Printf("[INFO] rollback: starting rollback manager")

	var mounts map[string]*uint32
	tick := time.NewTicker(m.Period)
	defer tick.Stop()
	for {
		// Wait for the tick
		<-tick.C

		// If we're quitting, then stop
		if atomic.LoadUint32(&m.running) != 1 {
			m.Logger.Printf("[INFO] rollback: stopping rollback manager")
			return
		}

		// Get the list of paths that we should rollback and setup our
		// mounts mapping. Mounts that have since been unmounted will
		// just "fall off" naturally: they aren't in our new mount mapping
		// and when their goroutine ends they'll naturally lose the reference.
		newMounts := make(map[string]*uint32)
		m.Mounts.RLock()
		for _, e := range m.Mounts.Entries {
			if s, ok := mounts[e.Path]; ok {
				newMounts[e.Path] = s
			} else {
				newMounts[e.Path] = new(uint32)
			}
		}
		m.Mounts.RUnlock()
		mounts = newMounts

		// Go through the mounts and start the rollback if we can
		for path, status := range mounts {
			// If we can change the status from 0 to 1, we can start it
			if !atomic.CompareAndSwapUint32(status, 0, 1) {
				continue
			}

			go m.rollback(path, status)
		}
	}
}

// Stop stops the running manager. This will not halt any in-flight
// rollbacks.
func (m *RollbackManager) Stop() {
	atomic.StoreUint32(&m.running, 0)
}

func (m *RollbackManager) rollback(path string, state *uint32) {
	defer atomic.StoreUint32(state, 0)

	m.Logger.Printf(
		"[DEBUG] rollback: starting rollback for %s",
		path)
	req := &logical.Request{
		Operation: logical.RollbackOperation,
		Path:      path,
	}
	if _, err := m.Router.Route(req); err != nil {
		m.Logger.Printf(
			"[ERR] rollback: error rolling back %s: %s",
			path, err)
	}
}

// The methods below are the hooks from core that are called pre/post seal.

func (c *Core) startRollback() error {
	// Ensure if we had a rollback it was stopped. This should never
	// be the case but it doesn't hurt to check.
	if c.rollback != nil {
		c.rollback.Stop()
	}

	c.rollback = &RollbackManager{
		Logger: c.logger,
		Router: c.router,
		Mounts: c.mounts,
		Period: 1 * time.Minute,
	}
	go c.rollback.Start()

	return nil
}

func (c *Core) stopRollback() error {
	if c.rollback != nil {
		c.rollback.Stop()
		c.rollback = nil
	}

	return nil
}
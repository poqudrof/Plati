package services

import (
	"fmt"
	"log"
	"time"

	"github.com/jmoiron/sqlx"

	"github.com/homaserver/plati/internal/auth"
	"github.com/homaserver/plati/internal/database/queries"
	"github.com/homaserver/plati/internal/incus"
	"github.com/homaserver/plati/internal/models"
)

// sleepCheckInterval is how often the worker sweeps for idle instances. It is
// also the worst-case overshoot on a deadline: an instance due to sleep right
// after a sweep waits for the next one.
const sleepCheckInterval = 5 * time.Minute

// maxSleepTimeoutMinutes caps a per-instance timeout at 30 days. Past that,
// "never" is what the user means — that is what Disabled is for.
const maxSleepTimeoutMinutes = 30 * 24 * 60

type SleepService struct {
	db       *sqlx.DB
	pool     *incus.Pool
	timeout  time.Duration
	stopChan chan struct{}
}

func NewSleepService(db *sqlx.DB, pool *incus.Pool, timeout time.Duration) *SleepService {
	return &SleepService{
		db:       db,
		pool:     pool,
		timeout:  timeout,
		stopChan: make(chan struct{}),
	}
}

func (s *SleepService) Start() {
	go s.run()
}

func (s *SleepService) Stop() {
	close(s.stopChan)
}

func (s *SleepService) run() {
	ticker := time.NewTicker(sleepCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopChan:
			return
		case <-ticker.C:
			s.sleepIdleInstances()
		}
	}
}

func (s *SleepService) sleepIdleInstances() {
	instances, err := queries.ListSleepableInstances(s.db, s.defaultMinutes())
	if err != nil {
		log.Printf("sleep worker: list sleepable instances: %v", err)
		return
	}

	for _, inst := range instances {
		server, err := queries.GetServer(s.db, inst.ServerID)
		if err != nil {
			continue
		}
		client, err := s.pool.GetClient(server.Name)
		if err != nil {
			continue
		}

		log.Printf("sleep worker: stopping idle instance %s (id=%d)", inst.IncusName, inst.ID)
		if err := client.StopInstance(inst.IncusName); err != nil {
			log.Printf("sleep worker: failed to stop %s: %v", inst.IncusName, err)
			continue
		}
		queries.UpdateInstanceStatus(s.db, inst.ID, "stopped")
	}
}

func (s *SleepService) defaultMinutes() int {
	return int(s.timeout.Minutes())
}

// SleepSettings is the auto-stop policy of one instance, resolved against the
// platform default and turned into the deadline the UI shows.
type SleepSettings struct {
	Disabled       bool `json:"disabled"`
	TimeoutMinutes int  `json:"timeout_minutes"` // 0 = follow the platform default
	DefaultMinutes int  `json:"default_minutes"`
	// EffectiveMinutes is what actually applies: TimeoutMinutes when set,
	// DefaultMinutes otherwise.
	EffectiveMinutes     int     `json:"effective_minutes"`
	CheckIntervalMinutes int     `json:"check_interval_minutes"`
	Status               string  `json:"status"`
	LastActiveAt         *string `json:"last_active_at"`
	// SleepsAt is the moment the worker becomes eligible to stop the instance,
	// or nil when nothing is scheduled (disabled, not running, or never started).
	SleepsAt *string `json:"sleeps_at"`
}

// GetSettings resolves the auto-stop policy of an instance the actor may reach.
func (s *SleepService) GetSettings(id int64, actor auth.Actor) (*SleepSettings, error) {
	inst, err := queries.GetInstanceForActor(s.db, id, actor)
	if err != nil {
		return nil, fmt.Errorf("instance not found")
	}
	return s.settingsFor(inst), nil
}

// UpdateSettings stores a new policy and returns it resolved, so the caller
// sees the deadline that now applies without a second round-trip.
func (s *SleepService) UpdateSettings(id int64, actor auth.Actor, disabled bool, timeoutMinutes int) (*SleepSettings, error) {
	if timeoutMinutes < 0 || timeoutMinutes > maxSleepTimeoutMinutes {
		return nil, fmt.Errorf("timeout_minutes must be between 0 and %d", maxSleepTimeoutMinutes)
	}
	inst, err := queries.GetInstanceForActor(s.db, id, actor)
	if err != nil {
		return nil, fmt.Errorf("instance not found")
	}
	// UpdateInstanceSleep filters on user_id, so it must carry the owner's id: an admin
	// editing someone else's policy would otherwise update zero rows and still get 200.
	if err := queries.UpdateInstanceSleep(s.db, id, inst.UserID, disabled, timeoutMinutes); err != nil {
		return nil, fmt.Errorf("save auto-stop settings: %w", err)
	}
	return s.GetSettings(id, actor)
}

// ResetTimer pushes the deadline back by a full timeout, for a user who is
// working in an instance the platform has no way of knowing is in use:
// last_active_at is only written on create and start, never by SSH or the
// web terminal.
func (s *SleepService) ResetTimer(id int64, actor auth.Actor) (*SleepSettings, error) {
	if _, err := queries.GetInstanceForActor(s.db, id, actor); err != nil {
		return nil, fmt.Errorf("instance not found")
	}
	if err := queries.UpdateInstanceLastActive(s.db, id); err != nil {
		return nil, fmt.Errorf("reset auto-stop timer: %w", err)
	}
	return s.GetSettings(id, actor)
}

func (s *SleepService) settingsFor(inst *models.Instance) *SleepSettings {
	effective := inst.SleepTimeoutMinutes
	if effective <= 0 {
		effective = s.defaultMinutes()
	}
	set := &SleepSettings{
		Disabled:             inst.SleepDisabled,
		TimeoutMinutes:       inst.SleepTimeoutMinutes,
		DefaultMinutes:       s.defaultMinutes(),
		EffectiveMinutes:     effective,
		CheckIntervalMinutes: int(sleepCheckInterval.Minutes()),
		Status:               inst.Status,
	}
	if inst.LastActiveAt.Valid {
		last := inst.LastActiveAt.Time.UTC()
		lastStr := last.Format("2006-01-02T15:04:05Z")
		set.LastActiveAt = &lastStr
		if !inst.SleepDisabled && inst.Status == "running" {
			at := last.Add(time.Duration(effective) * time.Minute).Format("2006-01-02T15:04:05Z")
			set.SleepsAt = &at
		}
	}
	return set
}

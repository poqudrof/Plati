package services

import (
	"fmt"
	"log"
	"time"

	"github.com/jmoiron/sqlx"

	"github.com/homaserver/plati/internal/database/queries"
	"github.com/homaserver/plati/internal/incus"
)

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
	ticker := time.NewTicker(5 * time.Minute)
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
	// Calculate the SQLite time modifier from the timeout
	modifier := fmt.Sprintf("-%d seconds", int(s.timeout.Seconds()))

	instances, err := queries.ListSleepableInstances(s.db, modifier)
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

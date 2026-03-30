package services

import (
	"github.com/jmoiron/sqlx"

	"github.com/homaserver/plati/internal/database/queries"
	"github.com/homaserver/plati/internal/incus"
	"github.com/homaserver/plati/internal/models"
)

type ServerService struct {
	db   *sqlx.DB
	pool *incus.Pool
}

func NewServerService(db *sqlx.DB, pool *incus.Pool) *ServerService {
	return &ServerService{db: db, pool: pool}
}

type ServerStatus struct {
	models.Server
	InstanceCount int `json:"instance_count"`
}

func (s *ServerService) List() ([]ServerStatus, error) {
	servers, err := queries.ListServers(s.db)
	if err != nil {
		return nil, err
	}

	result := make([]ServerStatus, len(servers))
	for i, srv := range servers {
		count, _ := queries.CountInstancesOnServer(s.db, srv.ID)
		result[i] = ServerStatus{
			Server:        srv,
			InstanceCount: count,
		}
	}
	return result, nil
}

func (s *ServerService) CheckHealth(serverName string) bool {
	client, err := s.pool.GetClient(serverName)
	if err != nil {
		return false
	}
	_, err = client.GetServerResources()
	return err == nil
}

func (s *ServerService) SyncServers(servers []models.Server) error {
	for _, srv := range servers {
		// Check if exists
		existing, _ := queries.ListServers(s.db)
		found := false
		for _, e := range existing {
			if e.Name == srv.Name {
				found = true
				break
			}
		}
		if !found {
			_, err := queries.CreateServer(s.db, srv.Name, srv.Endpoint, srv.TLSCertPath, srv.TLSKeyPath, srv.MaxInstances)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

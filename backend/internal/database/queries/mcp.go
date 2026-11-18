package queries

import (
	"database/sql"
	"time"

	"github.com/jmoiron/sqlx"
)

// RecordMCPInstanceCreation notes that instanceID was created through the MCP endpoint.
func RecordMCPInstanceCreation(db *sqlx.DB, userID, instanceID int64) error {
	_, err := db.Exec(
		"INSERT INTO mcp_instance_creations (user_id, instance_id) VALUES (?, ?)",
		userID, instanceID,
	)
	return err
}

// CountMCPInstances counts the instances created through MCP that userID still owns:
// deleted or transferred ones no longer count against the quota.
func CountMCPInstances(db *sqlx.DB, userID int64) (int, error) {
	var n int
	err := db.Get(&n, `SELECT COUNT(*) FROM mcp_instance_creations m
		JOIN instances i ON i.id = m.instance_id AND i.user_id = m.user_id
		WHERE m.user_id = ?`, userID)
	return n, err
}

// ListMCPInstanceIDs returns the ids of the instances userID created through MCP and
// still owns.
func ListMCPInstanceIDs(db *sqlx.DB, userID int64) ([]int64, error) {
	ids := []int64{}
	err := db.Select(&ids, `SELECT m.instance_id FROM mcp_instance_creations m
		JOIN instances i ON i.id = m.instance_id AND i.user_id = m.user_id
		WHERE m.user_id = ?`, userID)
	return ids, err
}

// MCPCreationsLastHour counts userID's MCP creations in the past hour, deleted
// instances included, and returns when the oldest of them leaves the window (zero
// when there is none).
func MCPCreationsLastHour(db *sqlx.DB, userID int64) (int, time.Time, error) {
	var row struct {
		N      int            `db:"n"`
		Oldest sql.NullString `db:"oldest"`
	}
	err := db.Get(&row, `SELECT COUNT(*) AS n, MIN(created_at) AS oldest FROM mcp_instance_creations
		WHERE user_id = ? AND created_at > datetime('now', '-1 hour')`, userID)
	if err != nil {
		return 0, time.Time{}, err
	}
	var freeAt time.Time
	if row.Oldest.Valid {
		for _, layout := range []string{"2006-01-02 15:04:05", time.RFC3339, "2006-01-02T15:04:05Z"} {
			if t, perr := time.Parse(layout, row.Oldest.String); perr == nil {
				freeAt = t.Add(time.Hour)
				break
			}
		}
	}
	return row.N, freeAt, nil
}

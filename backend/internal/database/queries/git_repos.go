package queries

import (
	"time"

	"github.com/jmoiron/sqlx"

	"github.com/homaserver/plati/internal/models"
)

func ListGitRepos(db *sqlx.DB) ([]models.GitRepo, error) {
	repos := []models.GitRepo{}
	err := db.Select(&repos, "SELECT * FROM git_repos ORDER BY name")
	return repos, err
}

func GetGitRepo(db *sqlx.DB, id int64) (*models.GitRepo, error) {
	var repo models.GitRepo
	err := db.Get(&repo, "SELECT * FROM git_repos WHERE id = ?", id)
	if err != nil {
		return nil, err
	}
	return &repo, nil
}

func GetGitRepoByName(db *sqlx.DB, name string) (*models.GitRepo, error) {
	var repo models.GitRepo
	err := db.Get(&repo, "SELECT * FROM git_repos WHERE name = ?", name)
	if err != nil {
		return nil, err
	}
	return &repo, nil
}

func CreateGitRepo(db *sqlx.DB, name, sshURL, localPath string) (int64, error) {
	res, err := db.Exec(
		`INSERT INTO git_repos (name, ssh_url, local_path, clone_status) VALUES (?, ?, ?, 'pending')`,
		name, sshURL, localPath,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func UpdateGitRepoStatus(db *sqlx.DB, id int64, status, errMsg string, syncedAt *time.Time) error {
	var syncedAtVal interface{}
	if syncedAt != nil {
		syncedAtVal = *syncedAt
	}
	_, err := db.Exec(
		`UPDATE git_repos SET clone_status = ?, error_message = ?, last_synced_at = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		status, errMsg, syncedAtVal, id,
	)
	return err
}

func RenameGitRepo(db *sqlx.DB, id int64, name string) error {
	_, err := db.Exec("UPDATE git_repos SET name = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?", name, id)
	return err
}

func DeleteGitRepo(db *sqlx.DB, id int64) error {
	_, err := db.Exec("DELETE FROM git_repos WHERE id = ?", id)
	return err
}

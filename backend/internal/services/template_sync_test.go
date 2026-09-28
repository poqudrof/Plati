package services

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/homaserver/plati/internal/database/queries"
)

func writeTemplateYAML(t *testing.T, dir, slug string) {
	t.Helper()
	body := "name: " + slug + "\nslug: " + slug + "\nimage: images:ubuntu/24.04/cloud\n"
	if err := os.WriteFile(filepath.Join(dir, slug+".yaml"), []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
}

// Archiving a template moves its YAML out of the templates dir. One that instances were
// built from cannot be deleted (instances.template_id is a foreign key, and Rebuild reads
// it): the sync must hide it instead of failing the DELETE on every reload.
func TestSyncFromDir_DeactivatesRemovedTemplateStillInUse(t *testing.T) {
	db := newTestDB(t)
	dir := t.TempDir()
	s := NewTemplateService(db, dir)

	writeTemplateYAML(t, dir, "used")
	writeTemplateYAML(t, dir, "unused")
	if err := s.SyncFromDir(dir); err != nil {
		t.Fatal(err)
	}

	used, err := queries.GetTemplateBySlug(db, "used")
	if err != nil {
		t.Fatal(err)
	}
	uid := mustCreateUser(t, db, "u@example.com", "U", "user")
	res, err := db.Exec(`INSERT INTO servers (name, endpoint) VALUES ('local', 'https://x')`)
	if err != nil {
		t.Fatal(err)
	}
	sid, _ := res.LastInsertId()
	if _, err := db.Exec(`INSERT INTO instances (name, user_id, template_id, server_id, incus_name) VALUES ('w', ?, ?, ?, 'plati-w')`,
		uid, used.ID, sid); err != nil {
		t.Fatal(err)
	}

	for _, slug := range []string{"used", "unused"} {
		if err := os.Remove(filepath.Join(dir, slug+".yaml")); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.SyncFromDir(dir); err != nil {
		t.Fatal(err)
	}

	if _, err := queries.GetTemplateBySlug(db, "unused"); err == nil {
		t.Error("unused template should have been deleted")
	}
	got, err := queries.GetTemplateBySlug(db, "used")
	if err != nil {
		t.Fatalf("template still in use was deleted: %v", err)
	}
	if got.IsActive {
		t.Error("template still in use should be deactivated")
	}

	// Restoring the YAML brings it back.
	writeTemplateYAML(t, dir, "used")
	if err := s.SyncFromDir(dir); err != nil {
		t.Fatal(err)
	}
	if got, _ := queries.GetTemplateBySlug(db, "used"); got == nil || !got.IsActive {
		t.Error("restored template should be active again")
	}
}

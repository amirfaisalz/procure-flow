package migrations

import (
	"strings"
	"testing"
	"testing/fstest"
)

func TestLoadFSParsesPairedMigrations(t *testing.T) {
	fsys := fstest.MapFS{
		"0001_create_procurements.up.sql":   {Data: []byte("CREATE TABLE procurements ();")},
		"0001_create_procurements.down.sql": {Data: []byte("DROP TABLE procurements;")},
		"0002_add_index.up.sql":             {Data: []byte("CREATE INDEX idx ON procurements (id);")},
		"0002_add_index.down.sql":           {Data: []byte("DROP INDEX idx;")},
		"README.md":                         {Data: []byte("ignored")},
	}

	list, err := loadFS(fsys)
	if err != nil {
		t.Fatalf("loadFS() error = %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("len(list) = %d, want 2", len(list))
	}
	if list[0].Version != "0001" || list[0].Name != "create_procurements" {
		t.Errorf("list[0] = %+v, want version 0001", list[0])
	}
	if list[1].Version != "0002" || !strings.Contains(list[1].Up, "CREATE INDEX") {
		t.Errorf("list[1] = %+v, want version 0002", list[1])
	}
}

func TestLoadFSRejectsMalformedSQLName(t *testing.T) {
	fsys := fstest.MapFS{
		"0001_create-procurements.up.sql":   {Data: []byte("SELECT 1;")},
		"0001_create-procurements.down.sql": {Data: []byte("SELECT 1;")},
	}

	if _, err := loadFS(fsys); err == nil {
		t.Fatal("loadFS() error = nil, want a filename validation error")
	}
}

func TestLoadFSRejectsMissingDownFile(t *testing.T) {
	fsys := fstest.MapFS{
		"0001_create_procurements.up.sql": {Data: []byte("SELECT 1;")},
	}

	if _, err := loadFS(fsys); err == nil {
		t.Fatal("loadFS() error = nil, want a missing down file error")
	}
}

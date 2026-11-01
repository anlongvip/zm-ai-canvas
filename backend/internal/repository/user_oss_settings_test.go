package repository

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"yingce/backend/internal/model"

	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestUserOSSSettingVersionsFollowSaveOrderAtSameClockTime(t *testing.T) {
	fixed := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "settings.db")), &gorm.Config{NowFunc: func() time.Time { return fixed }})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&model.UserOSSSetting{}); err != nil {
		t.Fatal(err)
	}
	repo := New(db)
	for _, setting := range []model.UserOSSSetting{
		{ID: "z-first", UserID: "owner", Enabled: true, ValueJSON: `{"cdnBaseURL":""}`},
		{ID: "a-second", UserID: "owner", Enabled: true, ValueJSON: `{"cdnBaseURL":"https://cdn.example.com"}`},
		{ID: "other-owner", UserID: "other", Enabled: true, ValueJSON: "{}"},
	} {
		if err := repo.CreateUserOSSSetting(&setting); err != nil {
			t.Fatal(err)
		}
	}
	latest, err := repo.LatestUserOSSSetting("owner")
	if err != nil {
		t.Fatal(err)
	}
	if latest.ID != "a-second" {
		t.Fatalf("latest=%q, want last saved version", latest.ID)
	}
	history, err := repo.UserOSSSettingsForUser("owner")
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 2 || history[0].ID != "a-second" || !history[0].CreatedAt.After(history[1].CreatedAt) {
		t.Fatalf("history is not ordered by save: %#v", history)
	}
	first, err := repo.UserOSSSettingForUser("owner", "z-first")
	if err != nil {
		t.Fatal(err)
	}
	if first.ValueJSON != `{"cdnBaseURL":""}` {
		t.Fatal("historical setting changed")
	}
	// A failed append must not consume a version or replace existing values.
	if err := repo.CreateUserOSSSetting(&model.UserOSSSetting{ID: "a-second", UserID: "owner", ValueJSON: "broken"}); err == nil {
		t.Fatal("duplicate setting ID unexpectedly saved")
	}
	third := &model.UserOSSSetting{ID: "third", UserID: "owner", ValueJSON: "{}"}
	if err := repo.CreateUserOSSSetting(third); err != nil {
		t.Fatal(err)
	}
	if !third.CreatedAt.Equal(latest.CreatedAt.Add(time.Microsecond)) {
		t.Fatalf("failed append changed version sequence: %v", third.CreatedAt)
	}
	// Wall-clock rollback must not make a newly saved version look older.
	future := fixed.Add(time.Hour)
	if err := db.Model(&model.UserOSSSetting{}).Where("id = ?", third.ID).Update("created_at", future).Error; err != nil {
		t.Fatal(err)
	}
	last := &model.UserOSSSetting{ID: "last", UserID: "owner", ValueJSON: "{}"}
	if err := repo.CreateUserOSSSetting(last); err != nil {
		t.Fatal(err)
	}
	if !last.CreatedAt.Equal(future.Add(time.Microsecond)) {
		t.Fatalf("wall-clock rollback broke save order: %v", last.CreatedAt)
	}
}

func TestUserOSSSettingConcurrentVersionsPostgres(t *testing.T) {
	dsn := os.Getenv("CANVAS_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set CANVAS_TEST_POSTGRES_DSN to an isolated test database")
	}
	fixed := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	config := &gorm.Config{Logger: logger.Default.LogMode(logger.Silent), NowFunc: func() time.Time { return fixed }}
	admin, err := gorm.Open(postgres.Open(dsn), config)
	if err != nil {
		t.Fatal(err)
	}
	closeTestDatabase(t, admin)
	schema := fmt.Sprintf("oss_versions_%d", time.Now().UnixNano())
	if err := admin.Exec("CREATE SCHEMA " + schema).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := admin.Exec("DROP SCHEMA " + schema + " CASCADE").Error; err != nil {
			t.Errorf("drop isolated OSS settings schema: %v", err)
		}
	})
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	db, err := gorm.Open(postgres.Open(parsed.String()), config)
	if err != nil {
		t.Fatal(err)
	}
	closeTestDatabase(t, db)
	if err := db.AutoMigrate(&model.UserOSSSetting{}); err != nil {
		t.Fatal(err)
	}
	repo := New(db)
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(8)
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	start := make(chan struct{})
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			errs <- repo.CreateUserOSSSetting(&model.UserOSSSetting{ID: fmt.Sprintf("version-%d", i), UserID: "owner", ValueJSON: "{}"})
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	history, err := repo.UserOSSSettingsForUser("owner")
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 8 {
		t.Fatalf("saved versions=%d, want 8", len(history))
	}
	for i, version := range history {
		want := fixed.Add(time.Duration(7-i) * time.Microsecond)
		if !version.CreatedAt.Equal(want) {
			t.Fatalf("version %d timestamp=%v, want %v", i, version.CreatedAt, want)
		}
	}
	latest, err := repo.LatestUserOSSSetting("owner")
	if err != nil || latest.ID != history[0].ID {
		t.Fatalf("latest version does not match history: %v, %v", latest, err)
	}
}

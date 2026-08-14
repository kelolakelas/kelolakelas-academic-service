package migration

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"gorm.io/gorm"

	"github.com/kelolakelas/kelolakelas-academic-service/internal/config"
	"github.com/kelolakelas/kelolakelas-academic-service/pkg/database"
)

func Run(path, direction string, steps int) error {
	if direction != "up" && direction != "down" {
		return errors.New("direction must be up or down")
	}
	if steps < 1 {
		return errors.New("steps must be greater than zero")
	}
	absolutePath, err := existingDirectory(path)
	if err != nil {
		return err
	}
	databaseURL, err := databaseURL()
	if err != nil {
		return err
	}
	migration, err := migrate.New((&url.URL{Scheme: "file", Path: absolutePath}).String(), databaseURL)
	if err != nil {
		return fmt.Errorf("migration initialization failed: %w", err)
	}
	defer migration.Close()
	if direction == "up" {
		err = migration.Up()
	} else {
		err = migration.Steps(-steps)
	}
	if errors.Is(err, migrate.ErrNoChange) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("migration failed: %w", err)
	}
	return nil
}

func Seed(directory, file string) error {
	files, err := seedFiles(directory, file)
	if err != nil {
		return err
	}
	cfg, err := config.LoadConfig()
	if err != nil {
		return errors.New("seed configuration unavailable")
	}
	db, err := database.NewPostgresDB(cfg.DBHost, cfg.DBPort, cfg.DBUser, cfg.DBPassword, cfg.DBName, cfg.DBSSLMode, cfg.DBChannelBinding)
	if err != nil {
		return errors.New("open database failed")
	}
	tx := db.Begin()
	if tx.Error != nil {
		return errors.New("begin seed transaction failed")
	}
	defer tx.Rollback()
	if err := tx.Exec(`CREATE TABLE IF NOT EXISTS seed_versions (filename varchar(255) PRIMARY KEY, checksum varchar(64) NOT NULL, applied_at timestamp NOT NULL DEFAULT now())`).Error; err != nil {
		return errors.New("prepare seed history failed")
	}
	pending := 0
	for _, seed := range files {
		var checksum string
		queryErr := tx.Raw("SELECT checksum FROM seed_versions WHERE filename = ?", seed.name).Scan(&checksum).Error
		if queryErr == nil && checksum != "" {
			if checksum != seed.checksum {
				return errors.New("seed file changed after it was applied")
			}
			continue
		}
		if queryErr != nil && !errors.Is(queryErr, gormRecordNotFound()) {
			return errors.New("read seed history failed")
		}
		if err := tx.Exec(string(seed.contents)).Error; err != nil {
			return errors.New("execute seed failed")
		}
		if err := tx.Exec("INSERT INTO seed_versions (filename, checksum) VALUES (?, ?)", seed.name, seed.checksum).Error; err != nil {
			return errors.New("record seed history failed")
		}
		pending++
	}
	if err := tx.Commit().Error; err != nil {
		return errors.New("commit seed failed")
	}
	_ = pending
	return nil
}

func CreateMigration(directory, name string) (string, error) {
	name, err := validName(name, "migration")
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return "", errors.New("create migration directory failed")
	}
	version, err := nextVersion(directory, 14)
	if err != nil {
		return "", err
	}
	prefix := fmt.Sprintf("%014d_%s", version, name)
	up, down := filepath.Join(directory, prefix+".up.sql"), filepath.Join(directory, prefix+".down.sql")
	if err := createEmpty(up); err != nil {
		return "", errors.New("create migration up file failed")
	}
	if err := createEmpty(down); err != nil {
		_ = os.Remove(up)
		return "", errors.New("create migration down file failed")
	}
	return fmt.Sprintf("created %s and %s", filepath.ToSlash(up), filepath.ToSlash(down)), nil
}

func CreateSeeder(directory, name string) (string, error) {
	name, err := validName(name, "seeder")
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return "", errors.New("create seed directory failed")
	}
	version, err := nextVersion(directory, 6)
	if err != nil {
		return "", err
	}
	path := filepath.Join(directory, fmt.Sprintf("%06d_%s.sql", version, name))
	if err := os.WriteFile(path, []byte("-- Add idempotent SQL statements here.\n"), 0o644); err != nil {
		return "", errors.New("create seed file failed")
	}
	return fmt.Sprintf("created %s", filepath.ToSlash(path)), nil
}

type seedFile struct {
	name     string
	contents []byte
	checksum string
}

func seedFiles(directory, file string) ([]seedFile, error) {
	if file != "" {
		seed, err := readSeed(file)
		if err != nil {
			return nil, err
		}
		return []seedFile{seed}, nil
	}
	if directory == "" {
		directory = "seeders"
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, errors.New("read seed directory failed")
	}
	files := make([]seedFile, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		seed, err := readSeed(filepath.Join(directory, entry.Name()))
		if err != nil {
			return nil, err
		}
		files = append(files, seed)
	}
	if len(files) == 0 {
		return nil, errors.New("no seed SQL files found")
	}
	sort.Slice(files, func(i, j int) bool { return files[i].name < files[j].name })
	return files, nil
}

func readSeed(path string) (seedFile, error) {
	contents, err := os.ReadFile(path)
	if err != nil || strings.TrimSpace(string(contents)) == "" {
		return seedFile{}, errors.New("seed file unavailable or empty")
	}
	sum := sha256.Sum256(contents)
	return seedFile{name: filepath.ToSlash(filepath.Clean(path)), contents: contents, checksum: hex.EncodeToString(sum[:])}, nil
}

func existingDirectory(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", errors.New("resolve migration path failed")
	}
	info, err := os.Stat(absolute)
	if err != nil || !info.IsDir() {
		return "", errors.New("migration path unavailable")
	}
	return absolute, nil
}

func databaseURL() (string, error) {
	cfg, err := config.LoadConfig()
	if err != nil {
		return "", err
	}
	return buildDatabaseURL(cfg)
}

func buildDatabaseURL(cfg config.Config) (string, error) {
	if cfg.DatabaseURL != "" {
		databaseURL, err := url.Parse(cfg.DatabaseURL)
		if err != nil {
			return "", err
		}
		query := databaseURL.Query()
		addChannelBinding(query, cfg.DBChannelBinding)
		databaseURL.RawQuery = query.Encode()
		return databaseURL.String(), nil
	}
	values := url.Values{"sslmode": []string{cfg.DBSSLMode}}
	addChannelBinding(values, cfg.DBChannelBinding)
	return (&url.URL{Scheme: "postgres", User: url.UserPassword(cfg.DBUser, cfg.DBPassword), Host: cfg.DBHost + ":" + cfg.DBPort, Path: "/" + cfg.DBName, RawQuery: values.Encode()}).String(), nil
}

func addChannelBinding(query url.Values, value string) {
	query.Del("channel_binding")
	if value == "prefer" || value == "require" {
		query.Set("channel_binding", value)
	}
}

func validName(name, kind string) (string, error) {
	name = strings.Join(strings.Fields(strings.TrimSpace(name)), "_")
	if name == "" {
		return "", fmt.Errorf("%s name is required", kind)
	}
	for _, r := range name {
		if !(r == '_' || r == '-' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
			return "", fmt.Errorf("%s name contains invalid characters", kind)
		}
	}
	return name, nil
}

func nextVersion(directory string, width int) (int64, error) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return 0, errors.New("read migration directory failed")
	}
	version := time.Now().UTC().Unix()
	if width == 6 {
		version = 0
	}
	for _, entry := range entries {
		parsed, err := strconv.ParseInt(strings.SplitN(entry.Name(), "_", 2)[0], 10, 64)
		if err == nil && parsed >= version {
			version = parsed + 1
		}
	}
	return version, nil
}

func createEmpty(path string) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	return file.Close()
}

func gormRecordNotFound() error { return gorm.ErrRecordNotFound }

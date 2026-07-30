package store

import (
	"database/sql"
	"errors"
	"time"

	_ "modernc.org/sqlite"
)

type Clip struct {
	ID              string    `json:"id"`
	Filepath        string    `json:"filepath"`
	ThumbnailPath   string    `json:"thumbnail_path"`
	CreatedAt       time.Time `json:"created_at"`
	GameTitle       string    `json:"game_title"`
	APMAtCapture    int       `json:"apm_at_capture"`
	DurationSeconds float64   `json:"duration_seconds"`
	SizeBytes       int64     `json:"size_bytes"`
	Shared          bool      `json:"shared"`
}

type Store struct {
	db *sql.DB
}

func Open(dsn string) (*Store, error) {
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}

	s := &Store{db: db}
	if err := s.Migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) Migrate() error {
	if s == nil || s.db == nil {
		return errors.New("store is not open")
	}

	const schema = `
CREATE TABLE IF NOT EXISTS clips (
    id TEXT PRIMARY KEY,
    filepath TEXT NOT NULL,
    thumbnail_path TEXT NOT NULL,
    created_at DATETIME NOT NULL,
    game_title TEXT NOT NULL,
    apm_at_capture INTEGER NOT NULL DEFAULT 0,
    duration_seconds REAL NOT NULL DEFAULT 0,
    size_bytes INTEGER NOT NULL DEFAULT 0,
    shared INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS apm_samples (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    sampled_at DATETIME NOT NULL,
    apm INTEGER NOT NULL
);`

	_, err := s.db.Exec(schema)
	return err
}

func (s *Store) InsertClip(clip Clip) error {
	if clip.CreatedAt.IsZero() {
		clip.CreatedAt = time.Now().UTC()
	}
	_, err := s.db.Exec(`
INSERT INTO clips (id, filepath, thumbnail_path, created_at, game_title, apm_at_capture, duration_seconds, size_bytes, shared)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		clip.ID,
		clip.Filepath,
		clip.ThumbnailPath,
		clip.CreatedAt.UTC(),
		clip.GameTitle,
		clip.APMAtCapture,
		clip.DurationSeconds,
		clip.SizeBytes,
		boolToInt(clip.Shared),
	)
	return err
}

func (s *Store) ListClips() ([]Clip, error) {
	rows, err := s.db.Query(`
SELECT id, filepath, thumbnail_path, created_at, game_title, apm_at_capture, duration_seconds, size_bytes, shared
FROM clips
ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var clips []Clip
	for rows.Next() {
		clip, err := scanClip(rows)
		if err != nil {
			return nil, err
		}
		clips = append(clips, clip)
	}
	return clips, rows.Err()
}

func (s *Store) GetClip(id string) (Clip, error) {
	row := s.db.QueryRow(`
SELECT id, filepath, thumbnail_path, created_at, game_title, apm_at_capture, duration_seconds, size_bytes, shared
FROM clips
WHERE id = ?`, id)
	return scanClip(row)
}

func (s *Store) MarkShared(id string) error {
	_, err := s.db.Exec(`UPDATE clips SET shared = 1 WHERE id = ?`, id)
	return err
}

func (s *Store) InsertAPMSample(apm int) error {
	_, err := s.db.Exec(`INSERT INTO apm_samples (sampled_at, apm) VALUES (?, ?)`, time.Now().UTC(), apm)
	return err
}

// TotalClipSizeBytes returns the sum of size_bytes for all unshared clips.
func (s *Store) TotalClipSizeBytes() (int64, error) {
	var total int64
	err := s.db.QueryRow(`SELECT COALESCE(SUM(size_bytes), 0) FROM clips WHERE shared = 0`).Scan(&total)
	return total, err
}

// DeleteOldestUnsharedClip removes the oldest unshared clip from the database and returns its file paths for deletion.
func (s *Store) DeleteOldestUnsharedClip() (filepath string, thumbPath string, err error) {
	row := s.db.QueryRow(`SELECT id, filepath, thumbnail_path FROM clips WHERE shared = 0 ORDER BY created_at ASC LIMIT 1`)
	var id string
	if err = row.Scan(&id, &filepath, &thumbPath); err != nil {
		return "", "", err
	}
	_, err = s.db.Exec(`DELETE FROM clips WHERE id = ?`, id)
	return filepath, thumbPath, err
}

type scanner interface {
	Scan(dest ...interface{}) error
}

func scanClip(s scanner) (Clip, error) {
	var clip Clip
	var shared int
	err := s.Scan(
		&clip.ID,
		&clip.Filepath,
		&clip.ThumbnailPath,
		&clip.CreatedAt,
		&clip.GameTitle,
		&clip.APMAtCapture,
		&clip.DurationSeconds,
		&clip.SizeBytes,
		&shared,
	)
	clip.Shared = shared != 0
	return clip, err
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

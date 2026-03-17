package buffer

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	_ "modernc.org/sqlite"
)

// BufferedActivity represents an activity stored in the local buffer
type BufferedActivity struct {
	ID          int64
	Email       string
	PayloadJSON string
	RetryCount  int
	LastRetryAt *time.Time
	CreatedAt   time.Time
	Status      string // pending, synced, failed
}

// ActivityBuffer handles local SQLite buffering of failed activity uploads
type ActivityBuffer struct {
	db     *sql.DB
	dbPath string
	mu     sync.Mutex
}

// NewActivityBuffer creates a new activity buffer using SQLite
func NewActivityBuffer() (*ActivityBuffer, error) {
	// Get AppData directory
	appData := os.Getenv("LOCALAPPDATA")
	if appData == "" {
		appData = os.Getenv("APPDATA")
	}
	if appData == "" {
		return nil, fmt.Errorf("could not find AppData folder")
	}

	// Create KTracker folder
	bufferDir := filepath.Join(appData, "KTracker")
	if err := os.MkdirAll(bufferDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create buffer directory: %w", err)
	}

	dbPath := filepath.Join(bufferDir, "buffer.db")

	// Open SQLite database
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open buffer database: %w", err)
	}

	// Set connection pool settings
	db.SetMaxOpenConns(1) // SQLite works best with single connection
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)

	buffer := &ActivityBuffer{
		db:     db,
		dbPath: dbPath,
	}

	// Initialize database schema
	if err := buffer.initSchema(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to initialize buffer schema: %w", err)
	}

	logrus.Infof("Activity buffer initialized at: %s", dbPath)
	return buffer, nil
}

// initSchema creates the database tables if they don't exist
func (b *ActivityBuffer) initSchema() error {
	schema := `
	CREATE TABLE IF NOT EXISTS buffered_activities (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		email TEXT NOT NULL,
		payload_json TEXT NOT NULL,
		retry_count INTEGER DEFAULT 0,
		last_retry_at INTEGER,
		created_at INTEGER NOT NULL,
		status TEXT DEFAULT 'pending'
	);

	CREATE INDEX IF NOT EXISTS idx_status ON buffered_activities(status);
	CREATE INDEX IF NOT EXISTS idx_created_at ON buffered_activities(created_at);
	`

	_, err := b.db.Exec(schema)
	if err != nil {
		return fmt.Errorf("failed to create schema: %w", err)
	}

	return nil
}

// BufferActivity stores a failed activity for later retry
func (b *ActivityBuffer) BufferActivity(email string, payload map[string]interface{}) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	_, err = b.db.Exec(
		`INSERT INTO buffered_activities (email, payload_json, created_at, status) VALUES (?, ?, ?, 'pending')`,
		email,
		string(payloadJSON),
		time.Now().Unix(),
	)
	if err != nil {
		return fmt.Errorf("failed to buffer activity: %w", err)
	}

	logrus.Debugf("Buffered activity for %s", email)
	return nil
}

// GetPendingActivities retrieves activities that need to be retried
func (b *ActivityBuffer) GetPendingActivities(limit int) ([]BufferedActivity, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	rows, err := b.db.Query(
		`SELECT id, email, payload_json, retry_count, last_retry_at, created_at, status
		 FROM buffered_activities
		 WHERE status = 'pending' AND retry_count < 10
		 ORDER BY created_at ASC
		 LIMIT ?`,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to query pending activities: %w", err)
	}
	defer rows.Close()

	var activities []BufferedActivity
	for rows.Next() {
		var a BufferedActivity
		var lastRetryAt sql.NullInt64
		var createdAtUnix int64

		err := rows.Scan(&a.ID, &a.Email, &a.PayloadJSON, &a.RetryCount, &lastRetryAt, &createdAtUnix, &a.Status)
		if err != nil {
			logrus.Warnf("Failed to scan buffered activity: %v", err)
			continue
		}

		a.CreatedAt = time.Unix(createdAtUnix, 0)
		if lastRetryAt.Valid {
			t := time.Unix(lastRetryAt.Int64, 0)
			a.LastRetryAt = &t
		}

		activities = append(activities, a)
	}

	return activities, nil
}

// MarkSynced marks an activity as successfully synced
func (b *ActivityBuffer) MarkSynced(id int64) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	_, err := b.db.Exec(
		`UPDATE buffered_activities SET status = 'synced' WHERE id = ?`,
		id,
	)
	if err != nil {
		return fmt.Errorf("failed to mark activity as synced: %w", err)
	}

	logrus.Debugf("Marked activity %d as synced", id)
	return nil
}

// IncrementRetryCount increments the retry count for a failed activity
func (b *ActivityBuffer) IncrementRetryCount(id int64) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	_, err := b.db.Exec(
		`UPDATE buffered_activities SET retry_count = retry_count + 1, last_retry_at = ? WHERE id = ?`,
		time.Now().Unix(),
		id,
	)
	if err != nil {
		return fmt.Errorf("failed to increment retry count: %w", err)
	}

	return nil
}

// MarkFailed marks an activity as permanently failed (exceeded retries)
func (b *ActivityBuffer) MarkFailed(id int64) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	_, err := b.db.Exec(
		`UPDATE buffered_activities SET status = 'failed' WHERE id = ?`,
		id,
	)
	if err != nil {
		return fmt.Errorf("failed to mark activity as failed: %w", err)
	}

	logrus.Warnf("Marked activity %d as permanently failed", id)
	return nil
}

// CleanupOld removes old synced and failed activities (older than 7 days)
func (b *ActivityBuffer) CleanupOld() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	cutoff := time.Now().Add(-7 * 24 * time.Hour).Unix()

	result, err := b.db.Exec(
		`DELETE FROM buffered_activities WHERE (status = 'synced' OR status = 'failed') AND created_at < ?`,
		cutoff,
	)
	if err != nil {
		return fmt.Errorf("failed to cleanup old activities: %w", err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected > 0 {
		logrus.Infof("Cleaned up %d old buffered activities", rowsAffected)
	}

	return nil
}

// Close closes the database connection
func (b *ActivityBuffer) Close() error {
	if b.db != nil {
		return b.db.Close()
	}
	return nil
}

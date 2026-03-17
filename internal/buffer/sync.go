package buffer

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// SyncWorker handles background syncing of buffered activities
type SyncWorker struct {
	buffer       *ActivityBuffer
	apiURL       string
	httpClient   *http.Client
	syncInterval time.Duration
	running      bool
	mu           sync.Mutex
	stopCh       chan struct{}
	wg           sync.WaitGroup
}

// NewSyncWorker creates a new sync worker
func NewSyncWorker(buffer *ActivityBuffer, apiURL string) *SyncWorker {
	return &SyncWorker{
		buffer: buffer,
		apiURL: apiURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		syncInterval: 60 * time.Second,
		stopCh:       make(chan struct{}),
	}
}

// Start starts the sync worker
func (w *SyncWorker) Start(ctx context.Context) error {
	w.mu.Lock()
	if w.running {
		w.mu.Unlock()
		return nil
	}
	w.running = true
	w.mu.Unlock()

	logrus.Info("Starting buffer sync worker")

	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		w.run(ctx)
	}()

	return nil
}

// run is the main sync loop
func (w *SyncWorker) run(ctx context.Context) {
	// Initial sync after short delay
	time.Sleep(10 * time.Second)
	w.syncPendingActivities()

	ticker := time.NewTicker(w.syncInterval)
	defer ticker.Stop()

	// Cleanup ticker (once per hour)
	cleanupTicker := time.NewTicker(1 * time.Hour)
	defer cleanupTicker.Stop()

	for {
		select {
		case <-ticker.C:
			w.syncPendingActivities()
		case <-cleanupTicker.C:
			w.buffer.CleanupOld()
		case <-ctx.Done():
			logrus.Info("Buffer sync worker stopping (context cancelled)")
			return
		case <-w.stopCh:
			logrus.Info("Buffer sync worker stopping (stop signal)")
			return
		}
	}
}

// syncPendingActivities attempts to sync buffered activities
func (w *SyncWorker) syncPendingActivities() {
	w.mu.Lock()
	apiURL := w.apiURL
	w.mu.Unlock()

	if apiURL == "" {
		logrus.Debug("Skipping sync - API URL not set")
		return
	}

	activities, err := w.buffer.GetPendingActivities(50)
	if err != nil {
		logrus.Errorf("Failed to get pending activities: %v", err)
		return
	}

	if len(activities) == 0 {
		return
	}

	logrus.Infof("Syncing %d buffered activities", len(activities))

	successCount := 0
	failCount := 0

	for _, activity := range activities {
		success := w.trySyncActivity(apiURL, activity)
		if success {
			if err := w.buffer.MarkSynced(activity.ID); err != nil {
				logrus.Warnf("Failed to mark activity %d as synced: %v", activity.ID, err)
			}
			successCount++
		} else {
			if err := w.buffer.IncrementRetryCount(activity.ID); err != nil {
				logrus.Warnf("Failed to increment retry count for activity %d: %v", activity.ID, err)
			}

			// Mark as permanently failed after 10 retries
			if activity.RetryCount >= 9 {
				if err := w.buffer.MarkFailed(activity.ID); err != nil {
					logrus.Warnf("Failed to mark activity %d as failed: %v", activity.ID, err)
				}
			}
			failCount++
		}

		// Small delay between retries to avoid hammering the server
		time.Sleep(100 * time.Millisecond)
	}

	logrus.Infof("Buffer sync complete: %d synced, %d failed", successCount, failCount)
}

// trySyncActivity attempts to sync a single buffered activity
func (w *SyncWorker) trySyncActivity(apiURL string, activity BufferedActivity) bool {
	// Send the stored payload directly
	resp, err := w.httpClient.Post(apiURL, "application/json", bytes.NewBufferString(activity.PayloadJSON))
	if err != nil {
		logrus.Debugf("Failed to send buffered activity %d: %v", activity.ID, err)
		return false
	}
	defer resp.Body.Close()

	// Read response
	body, _ := io.ReadAll(resp.Body)

	// Check HTTP status
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		logrus.Debugf("Buffered activity %d returned status %d", activity.ID, resp.StatusCode)
		return false
	}

	// Parse response
	var apiResponse struct {
		Success   bool   `json:"success"`
		Error     string `json:"error"`
		ErrorCode string `json:"error_code"`
	}

	if err := json.Unmarshal(body, &apiResponse); err != nil {
		logrus.Debugf("Failed to parse response for buffered activity %d: %v", activity.ID, err)
		return false
	}

	if !apiResponse.Success {
		logrus.Debugf("Buffered activity %d rejected: %s", activity.ID, apiResponse.Error)
		// Don't retry if it's an authentication error
		if apiResponse.ErrorCode == "invalid_credentials" || apiResponse.ErrorCode == "unauthorized" {
			// Mark as failed immediately
			w.buffer.MarkFailed(activity.ID)
		}
		return false
	}

	return true
}

// Stop stops the sync worker
func (w *SyncWorker) Stop() {
	w.mu.Lock()
	if !w.running {
		w.mu.Unlock()
		return
	}
	w.running = false
	w.mu.Unlock()

	close(w.stopCh)
	w.wg.Wait()
}


package digest

import (
	"context"
	"sync"
	"time"

	"github.com/Agnikulu/WikiSurge/internal/models"
	"github.com/Agnikulu/WikiSurge/internal/storage"
	"github.com/rs/zerolog"
)

// EmailSender is the interface for sending emails.
// Matches email.Sender so concrete senders from the email package can be used directly.
type EmailSender interface {
	Send(ctx context.Context, to, subject, htmlBody string) error
}

// SchedulerConfig controls when and how digests are sent.
type SchedulerConfig struct {
	DailySendHour      int  // UTC hour (0-23) to send daily digests
	WeeklySendDay      int  // Day of week (0=Sun, 1=Mon, ...)
	WeeklySendHour     int  // UTC hour to send weekly digests
	MaxConcurrentSends int  // Worker pool size
	DashboardURL       string
	Enabled            bool
}

// Scheduler orchestrates periodic digest email sending.
type Scheduler struct {
	collector  *Collector
	sender     EmailSender
	userStore  *storage.UserStore
	config     SchedulerConfig
	logger     zerolog.Logger
	stopCh     chan struct{}
	wg         sync.WaitGroup
}

// NewScheduler creates a new digest scheduler.
func NewScheduler(
	collector *Collector,
	sender EmailSender,
	userStore *storage.UserStore,
	cfg SchedulerConfig,
	logger zerolog.Logger,
) *Scheduler {
	if cfg.MaxConcurrentSends <= 0 {
		cfg.MaxConcurrentSends = 10
	}
	return &Scheduler{
		collector:  collector,
		sender:     sender,
		userStore:  userStore,
		config:     cfg,
		logger:     logger.With().Str("component", "digest-scheduler").Logger(),
		stopCh:     make(chan struct{}),
	}
}

// Start begins the scheduler loop. It checks every minute whether it's time to
// send daily or weekly digests. Call Stop() to shut down gracefully.
func (s *Scheduler) Start() {
	s.wg.Add(1)
	go s.loop()
	s.logger.Info().
		Int("daily_hour", s.config.DailySendHour).
		Int("weekly_day", s.config.WeeklySendDay).
		Int("weekly_hour", s.config.WeeklySendHour).
		Msg("Digest scheduler started")
}

// Stop signals the scheduler to stop and waits for it to finish.
func (s *Scheduler) Stop() {
	close(s.stopCh)
	s.wg.Wait()
	s.logger.Info().Msg("Digest scheduler stopped")
}

// RunNow immediately triggers a digest run for the given period.
// Useful for testing and manual triggers.
func (s *Scheduler) RunNow(ctx context.Context, period string) (sent, skipped, errored int) {
	return s.runDigest(ctx, period)
}

func (s *Scheduler) loop() {
	defer s.wg.Done()

	// Track which runs we've done today to avoid double-sending
	var lastDailyRun time.Time
	var lastWeeklyRun time.Time

	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopCh:
			return
		case now := <-ticker.C:
			now = now.UTC()

			// Daily digest
			if now.Hour() == s.config.DailySendHour && now.Minute() == 0 {
				if now.Sub(lastDailyRun) > 23*time.Hour {
					lastDailyRun = now
					s.logger.Info().Msg("Triggering daily digest run")
					go func() {
						ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
						defer cancel()
						sent, skipped, errored := s.runDigest(ctx, "daily")
						s.logger.Info().Int("sent", sent).Int("skipped", skipped).Int("errors", errored).Msg("Daily digest run complete")
					}()
				}
			}

			// Weekly digest
			if int(now.Weekday()) == s.config.WeeklySendDay && now.Hour() == s.config.WeeklySendHour && now.Minute() == 0 {
				if now.Sub(lastWeeklyRun) > 6*24*time.Hour {
					lastWeeklyRun = now
					s.logger.Info().Msg("Triggering weekly digest run")
					go func() {
						ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
						defer cancel()
						sent, skipped, errored := s.runDigest(ctx, "weekly")
						s.logger.Info().Int("sent", sent).Int("skipped", skipped).Int("errors", errored).Msg("Weekly digest run complete")
					}()
				}
			}
		}
	}
}

// runDigest executes one digest run for all eligible users.
func (s *Scheduler) runDigest(ctx context.Context, period string) (sent, skipped, errored int) {
	freq := models.DigestFreqDaily
	if period == "weekly" {
		freq = models.DigestFreqWeekly
	}

	// 1. Collect global data (one query, shared across all users)
	global, err := s.collector.CollectGlobal(ctx, period)
	if err != nil {
		s.logger.Error().Err(err).Str("period", period).Msg("Failed to collect global digest data")
		return 0, 0, 1
	}

	// 2. Get all eligible users
	users, err := s.userStore.GetUsersForDigest(freq)
	if err != nil {
		s.logger.Error().Err(err).Str("period", period).Msg("Failed to query users for digest")
		return 0, 0, 1
	}

	if len(users) == 0 {
		s.logger.Info().Str("period", period).Msg("No users eligible for digest")
		return 0, 0, 0
	}

	s.logger.Info().Int("users", len(users)).Str("period", period).Msg("Starting digest email batch")

	// 3. Process users with worker pool
	type result struct {
		status string // "sent", "skipped", "error"
	}
	results := make(chan result, len(users))
	userCh := make(chan *models.User, len(users))

	// Start workers
	var workerWg sync.WaitGroup
	for i := 0; i < s.config.MaxConcurrentSends; i++ {
		workerWg.Add(1)
		go func() {
			defer workerWg.Done()
			for user := range userCh {
				status := s.processUser(ctx, global, user)
				results <- result{status: status}
			}
		}()
	}

	// Feed users to workers
	for _, user := range users {
		userCh <- user
	}
	close(userCh)

	// Wait for all workers to finish, then close results
	go func() {
		workerWg.Wait()
		close(results)
	}()

	// Collect results
	for r := range results {
		switch r.status {
		case "sent":
			sent++
		case "skipped":
			skipped++
		case "error":
			errored++
		}
	}

	return sent, skipped, errored
}

// processUser handles one user: personalize, check threshold, render, send.
func (s *Scheduler) processUser(ctx context.Context, global *DigestData, user *models.User) string {
	// Personalize
	personalized := s.collector.PersonalizeForUser(ctx, global, user)

	// Check if worth sending
	if !s.collector.ShouldSendToUser(personalized, user) {
		return "skipped"
	}

	// Render email
	subject, htmlBody, err := RenderDigestEmail(personalized, user, s.config.DashboardURL, user.UnsubToken)
	if err != nil {
		s.logger.Error().Err(err).Str("user_id", user.ID).Msg("Failed to render digest email")
		return "error"
	}

	// Send
	if err := s.sender.Send(ctx, user.Email, subject, htmlBody); err != nil {
		s.logger.Error().Err(err).Str("user_id", user.ID).Str("email", user.Email).Msg("Failed to send digest email")
		return "error"
	}

	// Mark as sent
	if err := s.userStore.MarkDigestSent(user.ID, time.Now().UTC()); err != nil {
		s.logger.Warn().Err(err).Str("user_id", user.ID).Msg("Failed to mark digest sent (email was sent)")
	}

	return "sent"
}

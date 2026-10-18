package translatorbot

import (
	"context"
	"log"
	"sync"
	"time"
)

// defaultAutomatedEditDebounce is how long a bot or webhook message must stay
// unedited before its latest content is translated. Bots that animate a result
// by editing one message repeatedly therefore yield one translation of the
// settled state.
const defaultAutomatedEditDebounce = 5 * time.Second

// editDebouncer keeps at most one pending update per message. Each new edit
// replaces the pending update and restarts its delay.
type editDebouncer struct {
	mu     sync.Mutex
	delay  time.Duration
	timers map[string]*time.Timer
}

func newEditDebouncer(delay time.Duration) *editDebouncer {
	return &editDebouncer{delay: delay, timers: make(map[string]*time.Timer)}
}

func (d *editDebouncer) schedule(key string, fn func()) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if previous, ok := d.timers[key]; ok {
		previous.Stop()
	}
	var timer *time.Timer
	timer = time.AfterFunc(d.delay, func() {
		d.mu.Lock()
		if d.timers[key] != timer {
			d.mu.Unlock()
			return
		}
		delete(d.timers, key)
		d.mu.Unlock()
		fn()
	})
	d.timers[key] = timer
}

func (d *editDebouncer) cancel(key string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if timer, ok := d.timers[key]; ok {
		timer.Stop()
		delete(d.timers, key)
	}
}

func editDebounceKey(channelID, messageID string) string {
	return channelID + "\x00" + messageID
}

// ScheduleMessageUpdate handles a gateway edit. Human edits are translated
// immediately. Bot and webhook edits are translated once they have been quiet
// for the debounce period, using the latest content received.
func (s *Service) ScheduleMessageUpdate(ctx context.Context, m DiscordMessage) error {
	if !m.Bot && m.WebhookID == "" {
		return s.HandleMessageUpdate(ctx, m)
	}
	s.editDebounce.schedule(editDebounceKey(m.ChannelID, m.ID), func() {
		// The gateway event context ends when its handler returns, so the
		// deferred translation runs on its own context. Failures are logged
		// because the originating event has already been acknowledged.
		if err := s.HandleMessageUpdate(context.Background(), m); err != nil {
			log.Printf("debounced message update %s/%s: %v", m.ChannelID, m.ID, err)
		}
	})
	return nil
}

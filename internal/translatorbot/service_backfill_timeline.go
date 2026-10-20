package translatorbot

import (
	"fmt"
	"slices"
	"time"
)

// timelineReader merges the histories of several channels or threads into
// one oldest-first stream. It reads forward from each cursor and advances
// the cursor as messages are handed out.
type timelineReader struct {
	discord DiscordAPI
	guildID string
	ids     []string
	buffers map[string][]DiscordMessage
	done    map[string]bool
	cursors map[string]string
}

func newTimelineReader(discord DiscordAPI, guildID string, channelIDs []string, cursors map[string]string) *timelineReader {
	return &timelineReader{
		discord: discord, guildID: guildID, ids: channelIDs, cursors: cursors,
		buffers: map[string][]DiscordMessage{}, done: map[string]bool{},
	}
}

func (t *timelineReader) next() (DiscordMessage, bool, error) {
	best := ""
	for _, id := range t.ids {
		if len(t.buffers[id]) == 0 && !t.done[id] {
			after := t.cursors[id]
			if after == "" {
				after = "0"
			}
			page, err := t.discord.ChannelMessagePage(t.guildID, id, "", after)
			if err != nil {
				return DiscordMessage{}, false, fmt.Errorf("read history of %s: %w", id, err)
			}
			if len(page) < discordMessagePageLimit {
				t.done[id] = true
			}
			t.buffers[id] = page
		}
		if len(t.buffers[id]) == 0 {
			continue
		}
		if best == "" || snowflakeIDLess(t.buffers[id][0].ID, t.buffers[best][0].ID) {
			best = id
		}
	}
	if best == "" {
		return DiscordMessage{}, false, nil
	}
	m := t.buffers[best][0]
	t.buffers[best] = t.buffers[best][1:]
	t.cursors[best] = m.ID
	return m, true, nil
}

// nextStep makes a plain history a timeline of messages only.
func (t *timelineReader) nextStep() (timelineStep, bool, error) {
	m, ok, err := t.next()
	return timelineStep{message: m}, ok, err
}

// timelineStep is one event of a target channel in the order the source lived
// it: a message to mirror, or a source thread whose target thread is created
// here.
type timelineStep struct {
	message DiscordMessage
	family  *threadFamily
}

// timelineSource yields the steps of one target channel in order.
type timelineSource interface {
	nextStep() (timelineStep, bool, error)
}

// backfillTimeline merges the threads of a channel into its message history.
// A thread is created where the source thread was created, so the target
// channel announces it in place instead of after every message.
type backfillTimeline struct {
	messages *timelineReader
	// pending holds the threads still to create, earliest creation first.
	pending []*threadFamily
	// head is the next message; it is held back until no pending thread was
	// created before it.
	head DiscordMessage
	held bool
}

func newBackfillTimeline(messages *timelineReader, families []*threadFamily) *backfillTimeline {
	slices.SortFunc(families, compareThreadFamiliesByCreation)
	return &backfillTimeline{messages: messages, pending: families}
}

func (t *backfillTimeline) nextStep() (timelineStep, bool, error) {
	if !t.held {
		m, ok, err := t.messages.next()
		if err != nil {
			return timelineStep{}, false, err
		}
		t.head, t.held = m, ok
	}
	if len(t.pending) > 0 && t.creationDue(t.pending[0]) {
		family := t.pending[0]
		t.pending = t.pending[1:]
		return timelineStep{family: family}, true, nil
	}
	if !t.held {
		return timelineStep{}, false, nil
	}
	t.held = false
	return timelineStep{message: t.head}, true, nil
}

// creationDue reports whether the family's thread was created before the
// message the timeline holds back, or after the channel's last message.
func (t *backfillTimeline) creationDue(family *threadFamily) bool {
	if !t.held {
		return true
	}
	created, ok := threadFamilyCreatedAt(family)
	if !ok {
		return false
	}
	messagedAt, ok := discordSnowflakeTime(t.head.ID)
	// A thread created from a message goes right after that message, never
	// before it: only then does its starter message have a mirror to hang on.
	return ok && created.Before(messagedAt)
}

// threadFamilyCreatedAt is when the family's source thread was created. A
// thread started from a message falls back to the time of its starter message,
// which is what its id encodes.
func threadFamilyCreatedAt(family *threadFamily) (time.Time, bool) {
	rep := family.threads[0]
	if !rep.CreatedAt.IsZero() {
		return rep.CreatedAt, true
	}
	return discordSnowflakeTime(rep.ID)
}

// compareThreadFamiliesByCreation orders families by when their source thread
// was created. Threads without a reported creation time go last, in id order.
func compareThreadFamiliesByCreation(a, b *threadFamily) int {
	at, aok := threadFamilyCreatedAt(a)
	bt, bok := threadFamilyCreatedAt(b)
	switch {
	case aok && bok && !at.Equal(bt):
		return at.Compare(bt)
	case aok != bok:
		if aok {
			return -1
		}
		return 1
	}
	return compareSnowflakes(a.threads[0].ID, b.threads[0].ID)
}

// pendingThreadCreations lists the families whose target thread does not exist
// yet and whose source channel the timeline walks, so their creation belongs
// in that timeline. Forum posts and threads under an already created target
// thread stay with their family.
func pendingThreadCreations(families []*threadFamily, parentIDs []string) []*threadFamily {
	var out []*threadFamily
	for _, family := range families {
		if family.targetThreadID != "" || !slices.Contains(parentIDs, family.threads[0].ParentID) {
			continue
		}
		out = append(out, family)
	}
	return out
}

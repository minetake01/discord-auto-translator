package translatorbot

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"time"

	"github.com/bwmarrin/discordgo"
)

const (
	discordMessagePageLimit = 100

	discordCodeThreadArchived = 50083
	discordCodeThreadLocked   = 160005
)

// DiscordThread is a public thread or forum post as listed by Discord.
type DiscordThread struct {
	ID            string
	ParentID      string
	Name          string
	AppliedTags   []string
	Archived      bool
	Locked        bool
	LastMessageID string
}

// discordErrorCode returns the JSON error code of a Discord REST error, or 0.
func discordErrorCode(err error) int {
	var restErr *discordgo.RESTError
	if errors.As(err, &restErr) && restErr.Message != nil {
		return restErr.Message.Code
	}
	return 0
}

// isThreadClosedError reports whether Discord rejected an operation because
// the thread is archived or locked.
func isThreadClosedError(err error) bool {
	code := discordErrorCode(err)
	return code == discordCodeThreadArchived || code == discordCodeThreadLocked
}

// ChannelMessagePage returns up to 100 messages strictly before beforeID or
// strictly after afterID (at most one may be set; neither means the newest
// messages), sorted oldest first. Raw payloads are mapped so attachment alt
// text survives, and members are fetched for display names and role colors.
func (d DiscordGoAPI) ChannelMessagePage(guildID, channelID, beforeID, afterID string) ([]DiscordMessage, error) {
	if beforeID != "" && afterID != "" {
		return nil, errors.New("channel message page: before and after are mutually exclusive")
	}
	query := url.Values{}
	query.Set("limit", fmt.Sprint(discordMessagePageLimit))
	if beforeID != "" {
		query.Set("before", beforeID)
	}
	if afterID != "" {
		query.Set("after", afterID)
	}
	endpoint := discordgo.EndpointChannelMessages(channelID)
	body, err := withDiscordRetryValue(func() ([]byte, error) {
		return d.session.RequestWithBucketID("GET", endpoint+"?"+query.Encode(), nil, endpoint)
	})
	if err != nil {
		return nil, err
	}
	var raws []json.RawMessage
	if err := json.Unmarshal(body, &raws); err != nil {
		return nil, fmt.Errorf("decode channel messages: %w", err)
	}
	parentID, threadName := d.threadContext(channelID)
	out := make([]DiscordMessage, 0, len(raws))
	for _, raw := range raws {
		var message discordgo.Message
		if err := discordgo.Unmarshal(raw, &message); err != nil {
			return nil, fmt.Errorf("decode channel message: %w", err)
		}
		mapped, err := d.mapMessage(&message, raw, messageMapOptions{
			guildID: guildID, fetchMembers: true,
			knownThread: true, parentChannelID: parentID, threadName: threadName,
		})
		if err != nil {
			return nil, fmt.Errorf("map message %s/%s: %w", channelID, message.ID, err)
		}
		out = append(out, mapped)
	}
	slices.SortFunc(out, func(a, b DiscordMessage) int { return compareSnowflakes(a.ID, b.ID) })
	return out, nil
}

// PublicThreads lists the public threads and forum posts under parentIDs:
// the active ones and, when includeArchived is set, the archived ones.
// Private threads are never listed.
func (d DiscordGoAPI) PublicThreads(guildID string, parentIDs []string, includeArchived bool) ([]DiscordThread, error) {
	parents := make(map[string]bool, len(parentIDs))
	for _, id := range parentIDs {
		parents[id] = true
	}
	seen := map[string]bool{}
	var out []DiscordThread
	add := func(threads []*discordgo.Channel) {
		for _, ch := range threads {
			if ch == nil || seen[ch.ID] || !parents[ch.ParentID] {
				continue
			}
			if ch.Type != discordgo.ChannelTypeGuildPublicThread && ch.Type != discordgo.ChannelTypeGuildNewsThread {
				continue
			}
			seen[ch.ID] = true
			out = append(out, threadFromChannel(ch))
		}
	}
	active, err := withDiscordRetryValue(func() (*discordgo.ThreadsList, error) {
		return d.session.GuildThreadsActive(guildID)
	})
	if err != nil {
		return nil, fmt.Errorf("list active threads: %w", err)
	}
	add(active.Threads)
	if !includeArchived {
		return out, nil
	}
	for _, parentID := range parentIDs {
		var before *time.Time
		for {
			page, err := withDiscordRetryValue(func() (*discordgo.ThreadsList, error) {
				return d.session.ThreadsArchived(parentID, before, discordMessagePageLimit)
			})
			if err != nil {
				return nil, fmt.Errorf("list archived threads in %s: %w", parentID, err)
			}
			add(page.Threads)
			if !page.HasMore || len(page.Threads) == 0 {
				break
			}
			last := page.Threads[len(page.Threads)-1]
			if last.ThreadMetadata == nil {
				return nil, fmt.Errorf("archived thread %s has no metadata", last.ID)
			}
			stamp := last.ThreadMetadata.ArchiveTimestamp
			before = &stamp
		}
	}
	return out, nil
}

func threadFromChannel(ch *discordgo.Channel) DiscordThread {
	thread := DiscordThread{
		ID: ch.ID, ParentID: ch.ParentID, Name: ch.Name,
		AppliedTags: append([]string(nil), ch.AppliedTags...), LastMessageID: ch.LastMessageID,
	}
	if ch.ThreadMetadata != nil {
		thread.Archived = ch.ThreadMetadata.Archived
		thread.Locked = ch.ThreadMetadata.Locked
	}
	return thread
}

// SetThreadState changes the archived and/or locked flags of a thread.
func (d DiscordGoAPI) SetThreadState(threadID string, archived, locked *bool) error {
	_, err := withDiscordRetryValue(func() (*discordgo.Channel, error) {
		return d.session.ChannelEdit(threadID, &discordgo.ChannelEdit{Archived: archived, Locked: locked})
	})
	return err
}

func compareSnowflakes(a, b string) int {
	switch {
	case a == b:
		return 0
	case snowflakeIDLess(a, b):
		return -1
	default:
		return 1
	}
}

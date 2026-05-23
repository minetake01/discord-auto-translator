package translatorbot

import (
	"context"
	"errors"
	"testing"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := OpenStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestCreateGroupDefaultAndDuplicate(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	err := s.CreateGroupWithChannel(ctx, TranslationGroup{ID: "general", GuildID: "g1", DisplayName: "general", CreatedBy: "u1"}, GroupChannel{
		GroupID: "general", GuildID: "g1", ChannelID: "c1", ChannelType: 0, Language: "ja", WebhookID: "w1", WebhookToken: "t1",
	})
	if err != nil {
		t.Fatal(err)
	}
	err = s.CreateGroupWithChannel(ctx, TranslationGroup{ID: "general", GuildID: "g1", DisplayName: "general", CreatedBy: "u1"}, GroupChannel{
		GroupID: "general", GuildID: "g1", ChannelID: "c2", ChannelType: 0, Language: "en", WebhookID: "w2", WebhookToken: "t2",
	})
	if !errors.Is(err, ErrDuplicateGroup) {
		t.Fatalf("want ErrDuplicateGroup, got %v", err)
	}
}

func TestJoinChannelRejectsDuplicateLanguageAndChannel(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	err := s.CreateGroupWithChannel(ctx, TranslationGroup{ID: "team", GuildID: "g1", DisplayName: "team", CreatedBy: "u1"}, GroupChannel{
		GroupID: "team", GuildID: "g1", ChannelID: "c1", Language: "ja", WebhookID: "w1", WebhookToken: "t1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.JoinChannel(ctx, GroupChannel{GroupID: "team", GuildID: "g1", ChannelID: "c2", Language: "ja", WebhookID: "w2", WebhookToken: "t2"}); err == nil {
		t.Fatal("expected duplicate language error")
	}
	if err := s.JoinChannel(ctx, GroupChannel{GroupID: "team", GuildID: "g1", ChannelID: "c1", Language: "en", WebhookID: "w3", WebhookToken: "t3"}); err == nil {
		t.Fatal("expected duplicate channel error")
	}
}

func TestGroupAutocompleteQuery(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	for _, id := range []string{"general", "support"} {
		err := s.CreateGroupWithChannel(ctx, TranslationGroup{ID: id, GuildID: "g1", DisplayName: id, CreatedBy: "u1"}, GroupChannel{
			GroupID: id, GuildID: "g1", ChannelID: id + "-c", Language: id + "-lang", WebhookID: "w", WebhookToken: "t",
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.Groups(ctx, "g1", "gen", 25)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "general" {
		t.Fatalf("unexpected groups: %#v", got)
	}
}

func TestLeaveChannelRemovesChannelAndRelatedLinks(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.CreateGroupWithChannel(ctx, TranslationGroup{ID: "team", GuildID: "g1", DisplayName: "team", CreatedBy: "u1"}, GroupChannel{
		GroupID: "team", GuildID: "g1", ChannelID: "c1", Language: "ja", WebhookID: "w1", WebhookToken: "t1",
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.JoinChannel(ctx, GroupChannel{GroupID: "team", GuildID: "g1", ChannelID: "c2", Language: "en", WebhookID: "w2", WebhookToken: "t2"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveMessageLink(ctx, MessageLink{SourceMessageID: "m1", SourceChannelID: "c1", GroupID: "team", TargetChannelID: "c2", TargetMessageID: "m2"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveThreadLink(ctx, ThreadLink{GroupID: "team", SourceThreadID: "th1", SourceChannelID: "c1", TargetThreadID: "th2", TargetChannelID: "c2"}); err != nil {
		t.Fatal(err)
	}

	if err := s.LeaveChannel(ctx, "g1", "team", "c2"); err != nil {
		t.Fatal(err)
	}

	channels, err := s.ChannelsInGroup(ctx, "g1", "team")
	if err != nil {
		t.Fatal(err)
	}
	if len(channels) != 1 || channels[0].ChannelID != "c1" {
		t.Fatalf("unexpected channels: %#v", channels)
	}
	links, err := s.MessageTargets(ctx, "c1", "m1")
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 0 {
		t.Fatalf("message links were not removed: %#v", links)
	}
	threads, err := s.SourceThreadTargets(ctx, "th1")
	if err != nil {
		t.Fatal(err)
	}
	if len(threads) != 0 {
		t.Fatalf("thread links were not removed: %#v", threads)
	}
}

func TestDeleteGroupRemovesGroupChannelsAndLinks(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.CreateGroupWithChannel(ctx, TranslationGroup{ID: "team", GuildID: "g1", DisplayName: "team", CreatedBy: "u1"}, GroupChannel{
		GroupID: "team", GuildID: "g1", ChannelID: "c1", Language: "ja", WebhookID: "w1", WebhookToken: "t1",
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveMessageLink(ctx, MessageLink{SourceMessageID: "m1", SourceChannelID: "c1", GroupID: "team", TargetChannelID: "c2", TargetMessageID: "m2"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveThreadLink(ctx, ThreadLink{GroupID: "team", SourceThreadID: "th1", SourceChannelID: "c1", TargetThreadID: "th2", TargetChannelID: "c2"}); err != nil {
		t.Fatal(err)
	}

	if err := s.DeleteGroup(ctx, "g1", "team"); err != nil {
		t.Fatal(err)
	}

	groups, err := s.Groups(ctx, "g1", "team", 25)
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 0 {
		t.Fatalf("group was not deleted: %#v", groups)
	}
	channels, err := s.ChannelsInGroup(ctx, "g1", "team")
	if err != nil {
		t.Fatal(err)
	}
	if len(channels) != 0 {
		t.Fatalf("channels were not deleted: %#v", channels)
	}
	links, err := s.MessageTargets(ctx, "c1", "m1")
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 0 {
		t.Fatalf("message links were not removed: %#v", links)
	}
}

func TestRecentMessageHistoryReturnsUniquePreviousMessagesOldestFirst(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	for _, link := range []MessageLink{
		{SourceMessageID: "100", SourceChannelID: "ja", GroupID: "team", TargetChannelID: "en", TargetMessageID: "200", TargetLanguage: "en", SourceAuthorID: "a", SourceContentSnapshot: "first"},
		{SourceMessageID: "101", SourceChannelID: "ja", GroupID: "team", TargetChannelID: "en", TargetMessageID: "201", TargetLanguage: "en", SourceAuthorID: "b", SourceContentSnapshot: "second"},
		{SourceMessageID: "101", SourceChannelID: "ja", GroupID: "team", TargetChannelID: "fr", TargetMessageID: "301", TargetLanguage: "fr", SourceAuthorID: "b", SourceContentSnapshot: "second"},
		{SourceMessageID: "102", SourceChannelID: "ja", GroupID: "team", TargetChannelID: "en", TargetMessageID: "202", TargetLanguage: "en", SourceAuthorID: "c", SourceContentSnapshot: "current"},
	} {
		if err := s.SaveMessageLink(ctx, link); err != nil {
			t.Fatal(err)
		}
	}

	got, err := s.RecentMessageHistory(ctx, "ja", "102", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("history: %#v", got)
	}
	if got[0].SourceMessageID != "100" || got[1].SourceMessageID != "101" {
		t.Fatalf("unexpected order: %#v", got)
	}
}

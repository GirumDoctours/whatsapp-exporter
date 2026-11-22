package main

import (
	"context"
	"slices"
	"time"
)

const testChatLimit = 10

type rankedChat struct {
	group *chatGroup
	last  time.Time
}

func keepMatching[T any](items []T, keep func(T) bool) []T {
	kept := make([]T, 0, len(items))
	for _, item := range items {
		if keep(item) {
			kept = append(kept, item)
		}
	}
	return kept
}

func keepSelected[T any](items []T, only map[string]bool, chatOf func(T) string) []T {
	if only == nil {
		return items
	}
	return keepMatching(items, func(item T) bool { return only[chatOf(item)] })
}

func keepSelectedGroups(groups []*chatGroup, only map[string]bool) []*chatGroup {
	if only == nil {
		return groups
	}
	return keepMatching(groups, func(group *chatGroup) bool {
		return slices.ContainsFunc(group.raw, func(raw string) bool { return only[raw] })
	})
}

func latestChats(chats []chatRecord, names *nameResolver, limit int, lastVisible func(*chatGroup) (time.Time, error)) (map[string]bool, error) {
	names.learnChatAliases(chats)
	var ranked []rankedChat
	for _, group := range groupChats(chats, names) {
		if len(ranked) >= limit && !group.last.After(ranked[limit-1].last) {
			break
		}
		last, err := lastVisible(group)
		if err != nil {
			return nil, err
		}
		if last.IsZero() {
			continue
		}
		at, _ := slices.BinarySearchFunc(ranked, last, func(r rankedChat, t time.Time) int { return t.Compare(r.last) })
		ranked = slices.Insert(ranked, at, rankedChat{group: group, last: last})
	}
	selected := map[string]bool{}
	for _, chat := range ranked[:min(limit, len(ranked))] {
		for _, raw := range chat.group.raw {
			selected[raw] = true
		}
	}
	return selected, nil
}

func (e *exporter) selectTestChats(ctx context.Context) error {
	chats, err := e.arc.listChats(ctx)
	if err != nil {
		return err
	}
	names := newNameResolver(ctx, e.cli, "")
	e.onlyChats, err = latestChats(chats, names, testChatLimit, lastVisibleMessage(ctx, e.arc, names))
	return err
}

func lastVisibleMessage(ctx context.Context, arc *archive, names namer) func(*chatGroup) (time.Time, error) {
	return func(group *chatGroup) (time.Time, error) {
		msgs, err := arc.messagesFor(ctx, group.raw)
		if err != nil {
			return time.Time{}, err
		}
		return renderChat("", msgs, names, "", time.Local).Last, nil
	}
}

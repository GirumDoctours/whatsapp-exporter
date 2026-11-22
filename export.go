package main

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"go.mau.fi/whatsmeow/types"
)

const (
	groupFolderPrefix = "GP-"
	indexFileName     = "index.csv"
	reportFileName    = "report.txt"
	reportTimeLayout  = "02/01/2006 15:04:05"
)

type chatGroup struct {
	key      string
	raw      []string
	isGroup  bool
	archived bool
	name     string
	last     time.Time
}

type exportSummary struct {
	Chats       int
	Individuals int
	Groups      int
	Archived    int
	Messages    int
	Attachments int
	Missing     int
}

type indexRow struct {
	folder      string
	title       string
	group       *chatGroup
	profileName string
	rendered    renderedChat
}

func groupChats(chats []chatRecord, names *nameResolver) []*chatGroup {
	byKey := map[string]*chatGroup{}
	var order []*chatGroup
	for _, chat := range chats {
		jid, err := types.ParseJID(chat.JID)
		if err != nil || !isExportableChat(jid) {
			continue
		}
		isGroup := jid.Server == types.GroupServer
		key := jid.ToNonAD().String()
		if !isGroup {
			canonical := names.canonical(jid)
			if pn, err := types.ParseJID(chat.PN); err == nil && pn.Server == types.DefaultUserServer {
				canonical = pn.ToNonAD()
			}
			key = canonical.String()
		}
		group, ok := byKey[key]
		if !ok {
			group = &chatGroup{key: key, isGroup: isGroup}
			byKey[key] = group
			order = append(order, group)
		}
		group.raw = append(group.raw, chat.JID)
		group.archived = group.archived || chat.Archived
		if group.name == "" {
			group.name = chat.Name
		}
		if chat.LastMessage.After(group.last) {
			group.last = chat.LastMessage
		}
	}
	sort.SliceStable(order, func(i, j int) bool { return order[i].last.After(order[j].last) })
	return order
}

func availablePath(path string) string {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return path
	}
	ext := filepath.Ext(path)
	base := strings.TrimSuffix(path, ext)
	for i := 2; ; i++ {
		candidate := fmt.Sprintf("%s-%d%s", base, i, ext)
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate
		}
	}
}

func folderName(group *chatGroup, title string) string {
	if group.isGroup {
		return groupFolderPrefix + sanitizeFileName(title)
	}
	return sanitizeFileName(title)
}

func uniqueFolder(name string, used map[string]int) string {
	key := strings.ToLower(name)
	used[key]++
	if used[key] == 1 {
		return name
	}
	return fmt.Sprintf("%s-%d", name, used[key])
}

func writeExport(ctx context.Context, arc *archive, names *nameResolver, mediaDir, outPath, owner string, loc *time.Location, only map[string]bool, onProgress func(float64)) (exportSummary, error) {
	var summary exportSummary
	chats, err := arc.listChats(ctx)
	if err != nil {
		return summary, err
	}
	names.learnChatAliases(chats)
	groups := keepSelectedGroups(groupChats(chats, names), only)
	savedMedia, err := arc.savedMediaCounts(ctx)
	if err != nil {
		return summary, err
	}
	totalWork := 0
	for _, group := range groups {
		totalWork++
		for _, jid := range group.raw {
			totalWork += savedMedia[jid]
		}
	}
	workDone := 0
	advance := func() {
		workDone++
		onProgress(float64(workDone) / float64(max(1, totalWork)))
	}

	tmpPath := outPath + ".partial"
	file, err := os.Create(tmpPath)
	if err != nil {
		return summary, err
	}
	success := false
	defer func() {
		if !success {
			_ = file.Close()
			_ = os.Remove(tmpPath)
		}
	}()
	zw := zip.NewWriter(file)
	used := map[string]int{}
	var rows []indexRow

	for _, group := range groups {
		if err := ctx.Err(); err != nil {
			return summary, err
		}
		advance()
		msgs, err := arc.messagesFor(ctx, group.raw)
		if err != nil {
			return summary, err
		}
		for _, m := range msgs {
			names.rememberPushName(m)
		}
		title := names.chatTitle(group)
		rendered := renderChat(title, msgs, names, mediaDir, loc)
		if rendered.Messages == 0 {
			continue
		}
		folder := uniqueFolder(folderName(group, title), used)
		if err := addBytes(zw, folder+"/_chat.txt", []byte(rendered.Text), rendered.Last, true); err != nil {
			return summary, err
		}
		for _, attachment := range rendered.Attachments {
			if attachment.Source != "" {
				advance()
			}
			if err := ctx.Err(); err != nil {
				return summary, err
			}
			if err := addAttachment(zw, folder, attachment); err != nil {
				rendered.Missing = append(rendered.Missing, missingAttachment{When: attachment.Modified, Kind: "file", Reason: "could not be added: " + err.Error()})
				continue
			}
			summary.Attachments++
		}
		profile := ""
		if !group.isGroup {
			if jid, err := types.ParseJID(group.key); err == nil {
				profile = names.profileName(jid)
			}
		}
		rows = append(rows, indexRow{folder: folder, title: title, group: group, profileName: profile, rendered: rendered})
		summary.Chats++
		summary.Messages += rendered.Messages
		summary.Missing += len(rendered.Missing)
		if group.isGroup {
			summary.Groups++
		} else {
			summary.Individuals++
		}
		if group.archived {
			summary.Archived++
		}
	}

	if err := addBytes(zw, indexFileName, buildIndex(rows, loc), time.Now(), true); err != nil {
		return summary, err
	}
	if err := addBytes(zw, reportFileName, buildReport(rows, summary, owner, loc), time.Now(), true); err != nil {
		return summary, err
	}
	if err := zw.Close(); err != nil {
		return summary, err
	}
	if err := file.Close(); err != nil {
		return summary, err
	}
	if err := os.Rename(tmpPath, outPath); err != nil {
		return summary, err
	}
	success = true
	return summary, nil
}

func addBytes(zw *zip.Writer, name string, data []byte, modified time.Time, compress bool) error {
	header := &zip.FileHeader{Name: name, Method: zip.Store, Modified: modified}
	if compress {
		header.Method = zip.Deflate
	}
	w, err := zw.CreateHeader(header)
	if err != nil {
		return err
	}
	_, err = w.Write(data)
	return err
}

func addAttachment(zw *zip.Writer, folder string, attachment attachmentFile) error {
	name := folder + "/" + attachment.Name
	if attachment.Source == "" {
		return addBytes(zw, name, attachment.Data, attachment.Modified, true)
	}
	src, err := os.Open(attachment.Source)
	if err != nil {
		return err
	}
	defer src.Close()
	w, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Store, Modified: attachment.Modified})
	if err != nil {
		return err
	}
	_, err = io.Copy(w, src)
	return err
}

func buildIndex(rows []indexRow, loc *time.Location) []byte {
	var buf bytes.Buffer
	buf.WriteString("\xef\xbb\xbf")
	w := csv.NewWriter(&buf)
	_ = w.Write([]string{"Folder", "Chat", "Type", "Phone number or group ID", "WhatsApp profile name", "Archived", "Messages", "First message", "Last message", "Attachments saved", "Attachments missing"})
	for _, row := range rows {
		kind := "Individual"
		id := row.group.key
		if row.group.isGroup {
			kind = "Group"
		} else if jid, err := types.ParseJID(row.group.key); err == nil {
			id = firstNonEmpty(phoneLabel(jid), row.group.key)
		}
		archived := "No"
		if row.group.archived {
			archived = "Yes"
		}
		_ = w.Write([]string{
			row.folder,
			row.title,
			kind,
			id,
			row.profileName,
			archived,
			strconv.Itoa(row.rendered.Messages),
			row.rendered.First.In(loc).Format(reportTimeLayout),
			row.rendered.Last.In(loc).Format(reportTimeLayout),
			strconv.Itoa(len(row.rendered.Attachments)),
			strconv.Itoa(len(row.rendered.Missing)),
		})
	}
	w.Flush()
	return buf.Bytes()
}

func buildReport(rows []indexRow, summary exportSummary, owner string, loc *time.Location) []byte {
	var b strings.Builder
	nl := "\r\n"
	b.WriteString("WhatsApp export for +" + owner + nl)
	b.WriteString("Created " + time.Now().In(loc).Format(reportTimeLayout) + nl + nl)
	fmt.Fprintf(&b, "Chats: %s (%s individual, %s groups, %s archived)%s", formatCount(summary.Chats), formatCount(summary.Individuals), formatCount(summary.Groups), formatCount(summary.Archived), nl)
	fmt.Fprintf(&b, "Messages: %s%s", formatCount(summary.Messages), nl)
	fmt.Fprintf(&b, "Attachments saved: %s%s", formatCount(summary.Attachments), nl)
	fmt.Fprintf(&b, "Attachments missing: %s%s", formatCount(summary.Missing), nl)
	b.WriteString(nl + "History comes from the linked phone. Messages or files the phone no longer has could not be exported." + nl)
	if summary.Missing == 0 {
		return []byte(b.String())
	}
	b.WriteString(nl + "Missing attachments" + nl)
	for _, row := range rows {
		for _, missing := range row.rendered.Missing {
			fmt.Fprintf(&b, "%s | %s | %s | %s%s", row.folder, missing.When.In(loc).Format(reportTimeLayout), missing.Kind, missing.Reason, nl)
		}
	}
	return []byte(b.String())
}

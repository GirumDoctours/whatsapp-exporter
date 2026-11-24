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
	"sync"
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

type exportPlan struct {
	arc       *archive
	names     *nameResolver
	namer     namer
	mediaDir  string
	dir       string
	owner     string
	loc       *time.Location
	groups    []*chatGroup
	byRaw     map[string]*chatGroup
	folders   map[string]string
	onChat    func(chatInput)
	onFile    func(folder, path string)
	skipMedia bool

	mu      sync.Mutex
	rows    map[string]indexRow
	written map[string]bool
	summary exportSummary
}

func newExportPlan(ctx context.Context, arc *archive, names *nameResolver, mediaDir, dir, owner string, loc *time.Location, only map[string]bool) (*exportPlan, error) {
	chats, err := arc.listChats(ctx)
	if err != nil {
		return nil, err
	}
	names.learnChatAliases(chats)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	p := &exportPlan{
		arc: arc, names: names, namer: names, mediaDir: mediaDir, dir: dir, owner: owner, loc: loc,
		groups:  keepSelectedGroups(groupChats(chats, names), only),
		byRaw:   map[string]*chatGroup{},
		folders: map[string]string{},
		rows:    map[string]indexRow{},
		written: map[string]bool{},
	}
	used := map[string]int{}
	for _, g := range p.groups {
		p.folders[g.key] = uniqueFolder(folderName(g, names.chatTitle(g)), used)
		for _, raw := range g.raw {
			p.byRaw[raw] = g
		}
	}
	return p, nil
}

func (p *exportPlan) groupOf(chatJID string) *chatGroup {
	return p.byRaw[chatJID]
}

func (p *exportPlan) writeChat(ctx context.Context, group *chatGroup) error {
	msgs, err := p.arc.messagesFor(ctx, group.raw)
	if err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.written[group.key] {
		return nil
	}
	for _, m := range msgs {
		p.names.rememberPushName(m)
	}
	title := p.names.chatTitle(group)
	rendered := renderChatWith(title, msgs, p.namer, p.mediaDir, p.loc, p.skipMedia)
	p.written[group.key] = true
	if rendered.Messages == 0 {
		return nil
	}
	folder := p.folders[group.key]
	logExport.Debugf("Writing %q: %d messages, %d attachments, %d missing", folder, rendered.Messages, len(rendered.Attachments), len(rendered.Missing))
	dirPath := filepath.Join(p.dir, folder)
	if err := os.MkdirAll(dirPath, 0o700); err != nil {
		return err
	}
	if err := writeFileAt(filepath.Join(dirPath, "_chat.txt"), []byte(rendered.Text), rendered.Last); err != nil {
		return err
	}
	attachments := 0
	for _, attachment := range rendered.Attachments {
		if err := ctx.Err(); err != nil {
			return err
		}
		path := filepath.Join(dirPath, attachment.Name)
		if err := placeAttachment(path, attachment); err != nil {
			logExport.Warnf("Couldn't add %s to %q: %v", attachment.Name, folder, err)
			rendered.Missing = append(rendered.Missing, missingAttachment{When: attachment.Modified, Kind: "file", Reason: "could not be added: " + err.Error()})
			continue
		}
		attachments++
		if p.onFile != nil {
			p.onFile(folder, path)
		}
	}
	profile := ""
	if !group.isGroup {
		if jid, err := types.ParseJID(group.key); err == nil {
			profile = p.names.profileName(jid)
		}
	}
	p.rows[group.key] = indexRow{folder: folder, title: title, group: group, profileName: profile, rendered: rendered}
	p.summary.Chats++
	p.summary.Messages += rendered.Messages
	p.summary.Missing += len(rendered.Missing)
	p.summary.Attachments += attachments
	if group.isGroup {
		p.summary.Groups++
	} else {
		p.summary.Individuals++
	}
	if group.archived {
		p.summary.Archived++
	}
	if p.onChat != nil {
		p.onChat(chatInput{Folder: folder, Title: title, IsGroup: group.isGroup, Text: rendered.Text})
	}
	return nil
}

func (p *exportPlan) writeRemaining(ctx context.Context, onProgress func(float64)) error {
	for i, g := range p.groups {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := p.writeChat(ctx, g); err != nil {
			return err
		}
		if onProgress != nil {
			onProgress(float64(i+1) / float64(max(1, len(p.groups))))
		}
	}
	return nil
}

func (p *exportPlan) finish() (exportSummary, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	var rows []indexRow
	for _, g := range p.groups {
		if row, ok := p.rows[g.key]; ok {
			rows = append(rows, row)
		}
	}
	now := time.Now()
	if err := writeFileAt(filepath.Join(p.dir, indexFileName), buildIndex(rows, p.loc), now); err != nil {
		return p.summary, err
	}
	if err := writeFileAt(filepath.Join(p.dir, reportFileName), buildReport(rows, p.summary, p.owner, p.loc), now); err != nil {
		return p.summary, err
	}
	return p.summary, nil
}

func writeFileAt(path string, data []byte, modified time.Time) error {
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return err
	}
	if !modified.IsZero() {
		_ = os.Chtimes(path, modified, modified)
	}
	return nil
}

func placeAttachment(path string, attachment attachmentFile) error {
	if attachment.Source == "" {
		return writeFileAt(path, attachment.Data, attachment.Modified)
	}
	_ = os.Remove(path)
	if err := os.Link(attachment.Source, path); err != nil {
		if err := copyFile(attachment.Source, path); err != nil {
			return err
		}
	}
	if !attachment.Modified.IsZero() {
		_ = os.Chtimes(path, attachment.Modified, attachment.Modified)
	}
	return nil
}

func isHiddenExportPath(rel string) bool {
	for _, part := range strings.Split(filepath.ToSlash(rel), "/") {
		if strings.HasPrefix(part, ".") {
			return true
		}
	}
	return false
}

func compressedEntry(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".txt", ".csv", ".vcf":
		return true
	}
	return false
}

type zipSource struct {
	name     string
	path     string
	data     []byte
	modified time.Time
}

func folderEntries(dir string) ([]zipSource, error) {
	var entries []zipSource
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		if rel == "." {
			return nil
		}
		if isHiddenExportPath(rel) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() || !d.Type().IsRegular() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		entries = append(entries, zipSource{name: filepath.ToSlash(rel), path: path, modified: info.ModTime()})
		return nil
	})
	sort.Slice(entries, func(i, j int) bool { return entries[i].name < entries[j].name })
	return entries, err
}

func writeZip(outPath string, entries []zipSource, beforeClose func(zw *zip.Writer) error, onProgress func(float64)) error {
	tmpPath := outPath + ".partial"
	file, err := os.Create(tmpPath)
	if err != nil {
		return err
	}
	success := false
	defer func() {
		if !success {
			_ = file.Close()
			_ = os.Remove(tmpPath)
		}
	}()
	zw := zip.NewWriter(file)
	for i, e := range entries {
		if err := addZipEntry(zw, e); err != nil {
			return err
		}
		if onProgress != nil {
			onProgress(float64(i+1) / float64(len(entries)))
		}
	}
	if beforeClose != nil {
		if err := beforeClose(zw); err != nil {
			return err
		}
	}
	if err := zw.Close(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, outPath); err != nil {
		return err
	}
	success = true
	if info, err := os.Stat(outPath); err == nil {
		logZip.Infof("Wrote %s (%s, %d entries)", filepath.Base(outPath), humanBytes(info.Size()), len(entries))
	}
	return nil
}

func addZipEntry(zw *zip.Writer, e zipSource) error {
	if e.path == "" {
		return addBytes(zw, e.name, e.data, e.modified, compressedEntry(e.name))
	}
	src, err := os.Open(e.path)
	if err != nil {
		return err
	}
	defer src.Close()
	method := zip.Store
	if compressedEntry(e.name) {
		method = zip.Deflate
	}
	w, err := zw.CreateHeader(&zip.FileHeader{Name: e.name, Method: method, Modified: e.modified})
	if err != nil {
		return err
	}
	_, err = io.Copy(w, src)
	return err
}

func writeExport(ctx context.Context, arc *archive, names *nameResolver, mediaDir, outPath, owner string, loc *time.Location, only map[string]bool, onProgress func(float64)) (exportSummary, error) {
	dir, err := os.MkdirTemp(filepath.Dir(outPath), ".export-")
	if err != nil {
		return exportSummary{}, err
	}
	defer os.RemoveAll(dir)
	plan, err := newExportPlan(ctx, arc, names, mediaDir, dir, owner, loc, only)
	if err != nil {
		return exportSummary{}, err
	}
	if err := plan.writeRemaining(ctx, func(f float64) { onProgress(0.8 * f) }); err != nil {
		return plan.summary, err
	}
	summary, err := plan.finish()
	if err != nil {
		return summary, err
	}
	entries, err := folderEntries(dir)
	if err != nil {
		return summary, err
	}
	if err := writeZip(outPath, entries, nil, func(f float64) { onProgress(0.8 + 0.2*f) }); err != nil {
		return summary, err
	}
	onProgress(1)
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

package main

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	redactionReportName = "redaction-report.txt"
	redactionCacheDir   = ".redaction-cache"
)

type redactionTotals struct {
	Chats          int
	AIChats        int
	Slots          int
	High           int
	Flagged        int
	Disagreements  int
	Leaks          int
	Rebuilt        int
	WholeBody      int
	Fallback       int
	Renumbered     int
	AIFailedChunks int
	Unlocated      int
	TagCounts      map[string]int
	Media          mediaTotals
}

type redactionRun struct {
	r          *redactor
	dir        string
	owner      string
	dispatcher *batchDispatcher
	media      *mediaChecker
	mu         sync.Mutex
	outputs    map[string]chatOutput
	usage      func() string
}

func newRedactionRun(ctx context.Context, r *redactor, dir, owner string) *redactionRun {
	run := &redactionRun{r: r, dir: dir, owner: owner, outputs: map[string]chatOutput{}}
	run.dispatcher = newBatchDispatcher(ctx, redactBatchSize, r.redactChat, run.record)
	return run
}

func (run *redactionRun) record(out chatOutput) {
	run.mu.Lock()
	defer run.mu.Unlock()
	run.outputs[out.Folder] = out
}

func (run *redactionRun) output(folder string) (chatOutput, bool) {
	run.mu.Lock()
	defer run.mu.Unlock()
	out, ok := run.outputs[folder]
	return out, ok
}

type indexTable struct {
	header []string
	rows   [][]string
	byDir  map[string][]string
}

func readIndex(dir string) indexTable {
	table := indexTable{byDir: map[string][]string{}}
	data, err := os.ReadFile(filepath.Join(dir, indexFileName))
	if err != nil {
		return table
	}
	records, err := csv.NewReader(bytes.NewReader(bytes.TrimPrefix(data, []byte("\xef\xbb\xbf")))).ReadAll()
	if err != nil || len(records) == 0 {
		return table
	}
	table.header = records[0]
	table.rows = records[1:]
	for _, row := range table.rows {
		if len(row) > 0 {
			table.byDir[row[0]] = row
		}
	}
	return table
}

func chatFolders(dir string, index indexTable) []string {
	seen := map[string]bool{}
	var folders []string
	for _, row := range index.rows {
		if len(row) > 0 && !seen[row[0]] {
			if _, err := os.Stat(filepath.Join(dir, row[0], "_chat.txt")); err == nil {
				seen[row[0]] = true
				folders = append(folders, row[0])
			}
		}
	}
	entries, _ := os.ReadDir(dir)
	var extra []string
	for _, e := range entries {
		if e.IsDir() && !seen[e.Name()] && !strings.HasPrefix(e.Name(), ".") {
			if _, err := os.Stat(filepath.Join(dir, e.Name(), "_chat.txt")); err == nil {
				extra = append(extra, e.Name())
			}
		}
	}
	sort.Strings(extra)
	return append(folders, extra...)
}

func (run *redactionRun) queueMissing(index indexTable) {
	for _, folder := range chatFolders(run.dir, index) {
		if _, ok := run.output(folder); ok {
			continue
		}
		data, err := os.ReadFile(filepath.Join(run.dir, folder, "_chat.txt"))
		if err != nil {
			continue
		}
		title := ""
		isGroup := strings.HasPrefix(folder, groupFolderPrefix)
		if row, ok := index.byDir[folder]; ok && len(row) > 2 {
			title = row[1]
			isGroup = row[2] == "Group"
		}
		run.dispatcher.add(chatInput{Folder: folder, Title: title, IsGroup: isGroup, Text: string(data)})
	}
}

func (run *redactionRun) finish(ctx context.Context) {
	index := readIndex(run.dir)
	run.queueMissing(index)
	run.dispatcher.wait()
	if run.media != nil {
		run.media.wait()
	}
}

func roleOfTag(tag string, d *tagDictionary) string {
	if p, ok := d.parse(tag); ok && p.Kind == tagPerson {
		role := p.role()
		if p.Number >= 2 {
			role += "_" + strconv.Itoa(p.Number)
		}
		return role
	}
	return ""
}

func (run *redactionRun) partnerRole(out chatOutput) string {
	if tag, ok := out.SenderTags[out.Title]; ok {
		if role := roleOfTag(tag, run.r.dict); role != "" {
			return role
		}
	}
	if role := roleOfTag(out.TitleText, run.r.dict); role != "" {
		return role
	}
	return "CONTACT"
}

func (run *redactionRun) writeRedactedZip(outPath string, onProgress func(float64)) (redactionTotals, error) {
	totals := redactionTotals{TagCounts: map[string]int{}}
	index := readIndex(run.dir)
	folders := chatFolders(run.dir, index)
	used := map[string]int{}
	renamed := map[string]string{}
	titles := map[string]string{}
	groupTitles := 0
	var entries []zipSource
	now := time.Now()
	for _, folder := range folders {
		out, ok := run.output(folder)
		if !ok {
			return totals, fmt.Errorf("chat %q could not be redacted", folder)
		}
		title := out.TitleText
		if title == "{{GROUP_NAME}}" {
			groupTitles++
			if groupTitles > 1 {
				title = "{{GROUP_" + strconv.Itoa(groupTitles) + "_NAME}}"
			}
		}
		titles[folder] = title
		name := sanitizeFileName(title)
		if strings.HasPrefix(folder, groupFolderPrefix) {
			name = groupFolderPrefix + name
		}
		target := uniqueFolder(name, used)
		renamed[folder] = target
		run.addTotals(&totals, out)
		modified := now
		if info, err := os.Stat(filepath.Join(run.dir, folder, "_chat.txt")); err == nil {
			modified = info.ModTime()
		}
		entries = append(entries, zipSource{name: target + "/_chat.txt", data: []byte(out.Text), modified: modified})
		files, err := os.ReadDir(filepath.Join(run.dir, folder))
		if err != nil {
			return totals, err
		}
		for _, f := range files {
			if f.IsDir() || f.Name() == "_chat.txt" || strings.HasPrefix(f.Name(), ".") {
				continue
			}
			path := filepath.Join(run.dir, folder, f.Name())
			info, err := f.Info()
			if err != nil {
				return totals, err
			}
			entry := zipSource{name: target + "/" + f.Name(), path: path, modified: info.ModTime()}
			if strings.EqualFold(filepath.Ext(f.Name()), ".vcf") {
				if data, err := os.ReadFile(path); err == nil {
					entry.path, entry.data = "", []byte(redactVCard(string(data), out, run.r.dict, attachingRole(out, f.Name(), run.r.dict)))
				}
			} else if run.media != nil {
				if replacement, ok := run.media.replacement(path); ok {
					entry.path = replacement
				}
			}
			entries = append(entries, entry)
		}
	}
	if run.media != nil {
		totals.Media = run.media.totals()
	}
	entries = append(entries,
		zipSource{name: indexFileName, data: run.redactIndex(index, renamed, titles), modified: now},
		zipSource{name: reportFileName, data: run.redactReport(renamed), modified: now},
		zipSource{name: redactionReportName, data: run.reportText(totals), modified: now},
	)
	return totals, writeZip(outPath, entries, nil, onProgress)
}

func (run *redactionRun) addTotals(t *redactionTotals, out chatOutput) {
	t.Chats++
	if out.AIUsed {
		t.AIChats++
	}
	s := out.Stats
	t.Slots += s.Slots
	t.High += s.High
	t.Flagged += s.Flagged
	t.Disagreements += s.Disagreements
	t.Leaks += s.LeaksCaught
	t.Rebuilt += s.Rebuilt
	t.WholeBody += s.WholeBody
	t.Fallback += s.Fallback
	t.Renumbered += s.Renumbered
	t.AIFailedChunks += s.AIFailedChunks
	t.Unlocated += s.Unlocated
	for _, sl := range out.Slots {
		t.TagCounts[stripBraces(sl.Tag)]++
	}
	for _, tag := range out.SenderTags {
		for _, inner := range tagPattern.FindAllString(tag, -1) {
			t.TagCounts[stripBraces(inner)] += 0
		}
	}
}

func (run *redactionRun) redactIndex(index indexTable, renamed, titles map[string]string) []byte {
	var buf bytes.Buffer
	buf.WriteString("\xef\xbb\xbf")
	w := csv.NewWriter(&buf)
	if len(index.header) > 0 {
		_ = w.Write(index.header)
	}
	for _, row := range index.rows {
		if len(row) < 5 {
			continue
		}
		target, ok := renamed[row[0]]
		if !ok {
			continue
		}
		out, _ := run.output(row[0])
		copied := append([]string(nil), row...)
		copied[0] = target
		copied[1] = firstNonEmpty(titles[row[0]], out.TitleText)
		if row[2] == "Group" {
			copied[3] = "{{GROUP_ID}}"
		} else {
			role := run.partnerRole(out)
			copied[3] = roleTag(role, "PHONE")
			if copied[4] != "" {
				copied[4] = roleTag(role, "NICKNAME")
			}
		}
		if row[2] == "Group" && copied[4] != "" {
			copied[4] = "{{OTHER_PII}}"
		}
		_ = w.Write(copied)
	}
	w.Flush()
	return buf.Bytes()
}

func (run *redactionRun) redactReport(renamed map[string]string) []byte {
	data, err := os.ReadFile(filepath.Join(run.dir, reportFileName))
	if err != nil {
		return nil
	}
	text := string(data)
	if run.owner != "" {
		text = strings.ReplaceAll(text, "+"+run.owner, "{{OWNER_PHONE}}")
	}
	lines := strings.Split(text, "\r\n")
	for i, line := range lines {
		folder, rest, ok := strings.Cut(line, " | ")
		if !ok {
			continue
		}
		if target, found := renamed[folder]; found {
			lines[i] = target + " | " + rest
		}
	}
	return []byte(strings.Join(lines, "\r\n"))
}

func pct(part, whole int) string {
	if whole == 0 {
		return "0%"
	}
	return fmt.Sprintf("%.1f%%", 100*float64(part)/float64(whole))
}

func (run *redactionRun) reportText(t redactionTotals) []byte {
	var b strings.Builder
	nl := "\r\n"
	b.WriteString("Personal details redaction report" + nl)
	b.WriteString("Created " + time.Now().Format(reportTimeLayout) + nl + nl)
	fmt.Fprintf(&b, "Chats redacted: %s (%s checked by AI, %s with local checks only)%s", formatCount(t.Chats), formatCount(t.AIChats), formatCount(t.Chats-t.AIChats), nl)
	fmt.Fprintf(&b, "Details replaced with tags: %s%s", formatCount(t.Slots), nl)
	fmt.Fprintf(&b, "High confidence: %s (%s)%s", formatCount(t.High), pct(t.High, t.Slots), nl)
	fmt.Fprintf(&b, "Flagged as less certain: %s%s", formatCount(t.Flagged), nl)
	fmt.Fprintf(&b, "Times the two AI passes disagreed: %s%s", formatCount(t.Disagreements), nl)
	fmt.Fprintf(&b, "Details the second AI pass caught: %s%s", formatCount(t.Leaks), nl)
	fmt.Fprintf(&b, "Tags renumbered to stay consistent: %s%s", formatCount(t.Renumbered), nl)
	fmt.Fprintf(&b, "Messages rebuilt by the template check: %s%s", formatCount(t.Rebuilt), nl)
	fmt.Fprintf(&b, "Messages fully replaced because they could not be checked: %s%s", formatCount(t.WholeBody), nl)
	fmt.Fprintf(&b, "Generic tags used because nothing more specific fitted: %s%s", formatCount(t.Fallback), nl)
	fmt.Fprintf(&b, "AI chunks that failed and fell back to local checks: %s%s", formatCount(t.AIFailedChunks), nl)
	if m := t.Media; m.Checked+m.Removed > 0 {
		fmt.Fprintf(&b, nl+"Photos and PDFs checked: %s%s", formatCount(m.Checked), nl)
		fmt.Fprintf(&b, "ID documents replaced: %s%s", formatCount(m.IDDocuments), nl)
		fmt.Fprintf(&b, "PDFs with personal details replaced: %s%s", formatCount(m.Documents), nl)
		fmt.Fprintf(&b, "PDFs without readable text replaced: %s%s", formatCount(m.Unreadable), nl)
		fmt.Fprintf(&b, "Other files replaced because they can't be checked (documents, spreadsheets, archives): %s%s", formatCount(m.Removed), nl)
		fmt.Fprintf(&b, "Voice notes left out of the redacted copy: %s%s", formatCount(m.Audio), nl)
		fmt.Fprintf(&b, "Photos replaced because their text couldn't be read: %s%s", formatCount(m.Unreadable2), nl)
		fmt.Fprintf(&b, "Photos with text blacked out: %s%s", formatCount(m.Blacked), nl)
		fmt.Fprintf(&b, "Photos with GPS location removed: %s%s", formatCount(m.GPSRemoved), nl)
		for _, kind := range sortedKeys(m.Unchecked) {
			if kind != "skip" {
				fmt.Fprintf(&b, "Copied without checking: %s %s%s", formatCount(m.Unchecked[kind]), kind, nl)
			}
		}
		if m.Unavailable != "" {
			b.WriteString("Text recognition: " + m.Unavailable + nl)
		}
	}
	if run.usage != nil {
		b.WriteString(nl + "AI usage: " + run.usage() + nl)
	}
	if reason := run.r.aiStopReason(); reason != "" {
		b.WriteString("The AI check stopped early: " + reason + nl)
	}
	b.WriteString(nl + "Every message passed the template check: only tags were changed, and every tag is from the dictionary." + nl)
	b.WriteString(nl + "Tags used" + nl)
	type row struct {
		tag   string
		count int
	}
	var rows []row
	for tag, count := range t.TagCounts {
		if count > 0 {
			rows = append(rows, row{tag, count})
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].count != rows[j].count {
			return rows[i].count > rows[j].count
		}
		return rows[i].tag < rows[j].tag
	})
	for _, r := range rows {
		fmt.Fprintf(&b, "%8s  {{%s}}%s", formatCount(r.count), r.tag, nl)
	}
	return []byte(b.String())
}

var vcardLine = regexp.MustCompile(`^(?:item\d+\.)?([A-Za-z\-]+)((?:;[^:]*)?):(.*)$`)

func attachingRole(out chatOutput, fileName string, d *tagDictionary) string {
	marker := "<attached: " + fileName + ">"
	for i, l := range out.Lines {
		if !strings.Contains(l.Body, marker) {
			continue
		}
		for _, s := range out.Slots {
			if s.Line == i {
				if role := roleOfTag(s.Tag, d); role != "" {
					return role
				}
			}
		}
		for _, tag := range tagPattern.FindAllString(l.Body, -1) {
			if role := roleOfTag(tag, d); role != "" {
				return role
			}
		}
	}
	return ""
}

func redactVCard(data string, out chatOutput, d *tagDictionary, hint string) string {
	newline := "\n"
	if strings.Contains(data, "\r\n") {
		newline = "\r\n"
	}
	lines := strings.Split(strings.TrimRight(data, "\r\n"), newline)
	role := firstNonEmpty(hint, "PERSON")
	for _, line := range lines {
		if m := vcardLine.FindStringSubmatch(line); m != nil && strings.EqualFold(m[1], "FN") {
			want := normalizeValue(m[3])
			for _, s := range out.Slots {
				if normalizeValue(s.Value) == want && hint == "" {
					if r := roleOfTag(s.Tag, d); r != "" {
						role = r
					}
					break
				}
			}
		}
	}
	tag := func(attr string) string { return roleTag(role, attr) }
	var kept []string
	for _, line := range lines {
		m := vcardLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		key, params := strings.ToUpper(m[1]), m[2]
		value := ""
		switch key {
		case "BEGIN", "END", "VERSION", "PRODID":
			kept = append(kept, line)
			continue
		case "FN":
			value = tag("FULL_NAME")
		case "N":
			value = tag("LAST_NAME") + ";" + tag("FIRST_NAME") + ";;;"
		case "NICKNAME":
			value = tag("NICKNAME")
		case "TEL":
			value = tag("PHONE")
		case "EMAIL":
			value = tag("EMAIL")
		case "ADR":
			value = ";;" + tag("HOME_ADDRESS") + ";;;;"
		case "ORG":
			value = "{{COMPANY_NAME}}"
		case "TITLE":
			value = tag("JOB_TITLE")
		case "URL":
			value = tag("WEBSITE")
		case "BDAY":
			value = tag("BIRTH_DATE")
		case "NOTE":
			value = "{{OTHER_PII}}"
		default:
			continue
		}
		kept = append(kept, key+stripParamValues(params)+":"+value)
	}
	return strings.Join(kept, newline) + newline
}

var waidParam = regexp.MustCompile(`(?i);waid=[^;:]*`)

func stripParamValues(params string) string {
	return waidParam.ReplaceAllString(params, "")
}

package main

import (
	"encoding/json"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"unicode"

	"go.mau.fi/whatsmeow/types"
)

const peopleFileName = ".people.json"

type knownPerson struct {
	Key    string   `json:"key"`
	Own    bool     `json:"own,omitempty"`
	Labels []string `json:"labels,omitempty"`
	Names  []string `json:"names,omitempty"`
	Phones []string `json:"phones,omitempty"`
}

type peopleBook struct {
	mu        sync.Mutex
	OwnerKey  string                  `json:"owner_key"`
	OwnerCode string                  `json:"owner_country_code"`
	Persons   map[string]*knownPerson `json:"persons"`
	byLabel   map[string]string
	byPhone   map[string]string
	version   atomic.Int64
}

func (b *peopleBook) ownerValues() map[string]bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	values := map[string]bool{}
	if p, ok := b.Persons[b.OwnerKey]; ok {
		for _, list := range [][]string{p.Labels, p.Names, p.Phones} {
			for _, v := range list {
				values[normalizeValue(v)] = true
				for _, part := range strings.Fields(cleanName(v)) {
					values[normalizeValue(part)] = true
				}
			}
		}
	}
	return values
}

func newPeopleBook() *peopleBook {
	return &peopleBook{Persons: map[string]*knownPerson{}}
}

func (b *peopleBook) person(key string) *knownPerson {
	p, ok := b.Persons[key]
	if !ok {
		p = &knownPerson{Key: key}
		b.Persons[key] = p
	}
	return p
}

func (b *peopleBook) grew(before, after int) {
	if after != before {
		b.version.Add(1)
	}
}

func addUnique(list []string, values ...string) []string {
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		found := false
		for _, existing := range list {
			if existing == v {
				found = true
				break
			}
		}
		if !found {
			list = append(list, v)
		}
	}
	return list
}

func (b *peopleBook) addLabel(key, label string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	p := b.person(key)
	before := len(p.Labels) + len(p.Names)
	p.Labels = addUnique(p.Labels, label)
	if !strings.HasPrefix(label, "+") {
		p.Names = addUnique(p.Names, label)
	}
	b.grew(before, len(p.Labels)+len(p.Names))
	b.byLabel = nil
}

func (b *peopleBook) addNames(key string, names ...string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	p := b.person(key)
	before := len(p.Names)
	for _, n := range names {
		if n != "" && !strings.HasPrefix(n, "+") {
			p.Names = addUnique(p.Names, n)
		}
	}
	b.grew(before, len(p.Names))
}

func (b *peopleBook) addPhone(key, digits string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	p := b.person(key)
	p.Phones = addUnique(p.Phones, digits)
	b.byPhone = nil
}

func (b *peopleBook) index() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.byLabel != nil && b.byPhone != nil {
		return
	}
	b.byLabel = map[string]string{}
	b.byPhone = map[string]string{}
	for key, p := range b.Persons {
		for _, l := range p.Labels {
			b.byLabel[l] = key
		}
		for _, ph := range p.Phones {
			b.byPhone[ph] = key
		}
	}
}

func (b *peopleBook) keyForLabel(label string) string {
	b.index()
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.byLabel[label]
}

func (b *peopleBook) keyForPhone(digits string) string {
	b.index()
	b.mu.Lock()
	defer b.mu.Unlock()
	if key, ok := b.byPhone[digits]; ok {
		return key
	}
	local := strings.TrimLeft(digits, "0")
	for phone, key := range b.byPhone {
		if len(local) >= 8 && len(phone) >= 8 && (strings.HasSuffix(phone, local) || strings.HasSuffix(local, phone)) {
			return key
		}
	}
	return ""
}

func (b *peopleBook) keyForLID(digits string) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	key := "lid:" + digits
	if _, ok := b.Persons[key]; ok {
		return key
	}
	return ""
}

func (b *peopleBook) labelsOf(key string) []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if p, ok := b.Persons[key]; ok {
		return append([]string(nil), p.Labels...)
	}
	return nil
}

func (b *peopleBook) allLabels() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	var labels []string
	for _, p := range b.Persons {
		labels = append(labels, p.Labels...)
	}
	sort.Strings(labels)
	return labels
}

func (b *peopleBook) snapshot(keys map[string]bool) []*knownPerson {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []*knownPerson
	for key, p := range b.Persons {
		if keys == nil || keys[key] {
			copied := *p
			out = append(out, &copied)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

func (b *peopleBook) save(path string) error {
	b.mu.Lock()
	data, err := json.Marshal(b)
	b.mu.Unlock()
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func loadPeopleBook(path string) (*peopleBook, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	b := newPeopleBook()
	if err := json.Unmarshal(data, b); err != nil {
		return nil, err
	}
	if b.Persons == nil {
		b.Persons = map[string]*knownPerson{}
	}
	return b, nil
}

func personKey(jid types.JID) string {
	if jid.Server == types.DefaultUserServer {
		return jid.User
	}
	return jid.Server + ":" + jid.User
}

func onlyDigits(s string) string {
	var b strings.Builder
	for _, r := range s {
		if unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

type recordingNamer struct {
	inner *nameResolver
	book  *peopleBook
}

func (n recordingNamer) sender(m storedMessage) string {
	label := n.inner.sender(m)
	if m.FromMe {
		n.book.addLabel(n.book.OwnerKey, label)
		return label
	}
	raw := m.SenderJID
	if alt, err := types.ParseJID(m.SenderAltJID); err == nil && alt.Server == types.DefaultUserServer {
		raw = m.SenderAltJID
	}
	if jid, err := types.ParseJID(raw); err == nil && jid.User != "" {
		canonical := n.inner.canonical(jid)
		key := personKey(canonical)
		n.book.addLabel(key, label)
		n.book.addNames(key, m.PushName, n.inner.savedName(jid), n.inner.profileName(jid))
		if canonical.Server == types.DefaultUserServer {
			n.book.addPhone(key, canonical.User)
		}
	} else if m.PushName != "" {
		n.book.addLabel("push:"+m.PushName, label)
	}
	return label
}

func (n recordingNamer) person(raw string) string {
	label := n.inner.person(raw)
	if jid, err := types.ParseJID(raw); err == nil && jid.User != "" && !n.inner.isOwn(jid) {
		canonical := n.inner.canonical(jid)
		key := personKey(canonical)
		n.book.addLabel(key, label)
		n.book.addNames(key, n.inner.savedName(jid), n.inner.profileName(jid))
		if canonical.Server == types.DefaultUserServer {
			n.book.addPhone(key, canonical.User)
		}
	}
	return label
}

func (b *peopleBook) learnContacts(r *nameResolver) {
	for jid, info := range r.contacts {
		if !info.Found && info.PushName == "" {
			continue
		}
		canonical := r.canonical(jid)
		key := personKey(canonical)
		b.addNames(key, info.FullName, info.FirstName, info.BusinessName, info.PushName)
		if canonical.Server == types.DefaultUserServer {
			b.addPhone(key, canonical.User)
		}
	}
}

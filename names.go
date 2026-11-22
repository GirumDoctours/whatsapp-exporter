package main

import (
	"context"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/types"
)

type nameResolver struct {
	ctx        context.Context
	lids       store.LIDStore
	contacts   map[types.JID]types.ContactInfo
	ownName    string
	ownUsers   map[string]bool
	pushNames  map[string]string
	knownPhone map[types.JID]types.JID
}

func newNameResolver(ctx context.Context, cli *whatsmeow.Client, ownName string) *nameResolver {
	contacts, err := cli.Store.Contacts.GetAllContacts(ctx)
	if err != nil {
		contacts = map[types.JID]types.ContactInfo{}
	}
	own := map[string]bool{}
	if cli.Store.ID != nil {
		own[cli.Store.ID.User] = true
	}
	if !cli.Store.LID.IsEmpty() {
		own[cli.Store.LID.User] = true
	}
	return &nameResolver{
		ctx:       ctx,
		lids:      cli.Store.LIDs,
		contacts:  contacts,
		ownName:   ownName,
		ownUsers:  own,
		pushNames: map[string]string{},
	}
}

func (r *nameResolver) learnChatAliases(chats []chatRecord) {
	if r.knownPhone == nil {
		r.knownPhone = map[types.JID]types.JID{}
	}
	for _, chat := range chats {
		pn, err := types.ParseJID(chat.PN)
		if err != nil || pn.Server != types.DefaultUserServer {
			continue
		}
		for _, raw := range []string{chat.JID, chat.LID} {
			if lid, err := types.ParseJID(raw); err == nil && lid.Server == types.HiddenUserServer {
				r.knownPhone[lid.ToNonAD()] = pn.ToNonAD()
			}
		}
	}
}

func (r *nameResolver) canonical(jid types.JID) types.JID {
	jid = jid.ToNonAD()
	if pn, ok := r.knownPhone[jid]; ok {
		return pn
	}
	if jid.Server == types.HiddenUserServer && r.lids != nil {
		if pn, err := r.lids.GetPNForLID(r.ctx, jid); err == nil && !pn.IsEmpty() {
			return pn.ToNonAD()
		}
	}
	return jid
}

func (r *nameResolver) aliases(jid types.JID) []types.JID {
	jid = jid.ToNonAD()
	result := []types.JID{jid}
	if canonical := r.canonical(jid); canonical != jid {
		result = append(result, canonical)
	}
	if jid.Server == types.DefaultUserServer && r.lids != nil {
		if lid, err := r.lids.GetLIDForPN(r.ctx, jid); err == nil && !lid.IsEmpty() {
			result = append(result, lid.ToNonAD())
		}
	}
	return result
}

func (r *nameResolver) savedName(jid types.JID) string {
	for _, alias := range r.aliases(jid) {
		if info, ok := r.contacts[alias]; ok && info.Found {
			if name := firstNonEmpty(info.FullName, info.FirstName, info.BusinessName); name != "" {
				return name
			}
		}
	}
	return ""
}

func (r *nameResolver) profileName(jid types.JID) string {
	for _, alias := range r.aliases(jid) {
		if info, ok := r.contacts[alias]; ok && info.PushName != "" {
			return info.PushName
		}
	}
	return r.pushNames[r.canonical(jid).String()]
}

func (r *nameResolver) rememberPushName(m storedMessage) {
	if m.FromMe || m.PushName == "" || m.IsGroup && m.SenderJID == "" {
		return
	}
	jid, err := types.ParseJID(m.SenderJID)
	if err != nil {
		return
	}
	r.pushNames[r.canonical(jid).String()] = m.PushName
}

func (r *nameResolver) isOwn(jid types.JID) bool {
	return r.ownUsers[jid.User]
}

func phoneLabel(jid types.JID) string {
	if jid.Server == types.DefaultUserServer {
		return "+" + jid.User
	}
	return ""
}

func (r *nameResolver) labelFor(jid types.JID, pushName string) string {
	if name := r.savedName(jid); name != "" {
		return name
	}
	if phone := phoneLabel(r.canonical(jid)); phone != "" {
		return phone
	}
	if pushName != "" {
		return pushName
	}
	if profile := r.profileName(jid); profile != "" {
		return profile
	}
	return "Unknown"
}

func (r *nameResolver) sender(m storedMessage) string {
	if m.FromMe {
		return r.ownName
	}
	raw := m.SenderJID
	if alt, err := types.ParseJID(m.SenderAltJID); err == nil && alt.Server == types.DefaultUserServer {
		raw = m.SenderAltJID
	}
	jid, err := types.ParseJID(raw)
	if err != nil {
		return firstNonEmpty(m.PushName, "Unknown")
	}
	return r.labelFor(jid, m.PushName)
}

func (r *nameResolver) person(raw string) string {
	jid, err := types.ParseJID(raw)
	if err != nil || jid.User == "" {
		return raw
	}
	if r.isOwn(jid) {
		return "You"
	}
	return r.labelFor(jid, "")
}

func (r *nameResolver) chatTitle(g *chatGroup) string {
	if g.isGroup {
		return firstNonEmpty(g.name, "Group")
	}
	jid, err := types.ParseJID(g.key)
	if err != nil {
		return firstNonEmpty(g.name, g.key)
	}
	if name := r.savedName(jid); name != "" {
		return name
	}
	if g.name != "" {
		return g.name
	}
	return r.labelFor(jid, "")
}

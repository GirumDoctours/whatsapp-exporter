package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/proto/waWeb"
)

const (
	lrm            = "\u200e"
	e2eNotice      = "Messages and calls are end-to-end encrypted. Only people in this chat can read, listen to, or share them."
	editedMarker   = "<This message was edited>"
	chatTimeLayout = "02/01/2006, 15:04:05"
)

type namer interface {
	sender(m storedMessage) string
	person(jid string) string
}

type attachmentFile struct {
	Name     string
	Source   string
	Data     []byte
	Modified time.Time
}

type missingAttachment struct {
	When   time.Time
	Kind   string
	Reason string
}

type renderedChat struct {
	Text        string
	Attachments []attachmentFile
	Missing     []missingAttachment
	Messages    int
	First       time.Time
	Last        time.Time
}

type chatRenderer struct {
	title     string
	skipMedia bool
	names     namer
	mediaDir  string
	loc       *time.Location
	edits     map[string]*waE2E.Message
	revoked   map[string]bool
	seq       int
	lines     []string
	out       renderedChat
}

func renderChat(title string, msgs []storedMessage, names namer, mediaDir string, loc *time.Location) renderedChat {
	return renderChatWith(title, msgs, names, mediaDir, loc, false)
}

func renderChatWith(title string, msgs []storedMessage, names namer, mediaDir string, loc *time.Location, skipMedia bool) renderedChat {
	r := &chatRenderer{
		skipMedia: skipMedia,
		title:     title,
		names:     names,
		mediaDir:  mediaDir,
		loc:       loc,
		edits:     map[string]*waE2E.Message{},
		revoked:   map[string]bool{},
	}
	r.collectEditsAndDeletes(msgs)
	for _, m := range msgs {
		sender, body, ok := r.entry(m)
		if !ok {
			continue
		}
		if len(r.lines) == 0 {
			r.lines = append(r.lines, r.line(m.Timestamp, r.title, lrm+e2eNotice))
		}
		r.lines = append(r.lines, r.line(m.Timestamp, sender, body))
		r.out.Messages++
		if r.out.First.IsZero() {
			r.out.First = m.Timestamp
		}
		r.out.Last = m.Timestamp
	}
	if len(r.lines) > 0 {
		r.out.Text = strings.ReplaceAll(strings.Join(r.lines, "\n"), "\n", "\r\n") + "\r\n"
	}
	return r.out
}

func protocolMessageOf(msg *waE2E.Message) *waE2E.ProtocolMessage {
	if pm := msg.GetProtocolMessage(); pm != nil {
		return pm
	}
	return msg.GetEditedMessage().GetMessage().GetProtocolMessage()
}

func (r *chatRenderer) collectEditsAndDeletes(msgs []storedMessage) {
	for _, m := range msgs {
		pm := protocolMessageOf(m.Message)
		if pm == nil {
			continue
		}
		target := pm.GetKey().GetID()
		switch pm.GetType() {
		case waE2E.ProtocolMessage_REVOKE:
			r.revoked[target] = true
		case waE2E.ProtocolMessage_MESSAGE_EDIT:
			if edited := pm.GetEditedMessage(); edited != nil {
				r.edits[target] = edited
			}
		}
	}
}

func (r *chatRenderer) line(ts time.Time, sender, body string) string {
	body = strings.ReplaceAll(body, "\r\n", "\n")
	return fmt.Sprintf("[%s] %s: %s", ts.In(r.loc).Format(chatTimeLayout), sender, body)
}

func deletedNotice(m storedMessage) string {
	if m.FromMe {
		return lrm + "You deleted this message."
	}
	return lrm + "This message was deleted."
}

func (r *chatRenderer) senderOrTitle(m storedMessage) string {
	if !m.FromMe && m.SenderJID == "" {
		return firstNonEmpty(m.PushName, r.title)
	}
	return r.names.sender(m)
}

func (r *chatRenderer) entry(m storedMessage) (sender, body string, ok bool) {
	if waWeb.WebMessageInfo_StubType(m.StubType) == waWeb.WebMessageInfo_REVOKE {
		return r.senderOrTitle(m), deletedNotice(m), true
	}
	if m.StubType != 0 {
		body, ok = r.systemText(m)
		return r.title, body, ok
	}
	if m.Message == nil {
		return "", "", false
	}
	if protocolMessageOf(m.Message) != nil {
		return "", "", false
	}
	sender = r.senderOrTitle(m)
	if r.revoked[m.ID] {
		return sender, deletedNotice(m), true
	}
	edit, edited := r.edits[m.ID]
	body, ok = r.content(m, edit, edited)
	return sender, body, ok
}

func editedText(msg *waE2E.Message) string {
	return firstNonEmpty(
		msg.GetConversation(),
		msg.GetExtendedTextMessage().GetText(),
		msg.GetImageMessage().GetCaption(),
		msg.GetVideoMessage().GetCaption(),
		msg.GetDocumentMessage().GetCaption(),
	)
}

func (r *chatRenderer) content(m storedMessage, edit *waE2E.Message, edited bool) (string, bool) {
	msg := contentOf(m.Message)
	suffix := ""
	if edited {
		suffix = " " + lrm + editedMarker
	}
	if media, ok := findMedia(msg); ok {
		caption := media.caption
		if edited {
			caption = editedText(edit)
		}
		return r.mediaBody(m, media, caption) + suffix, true
	}
	if text := firstNonEmpty(msg.GetConversation(), msg.GetExtendedTextMessage().GetText()); text != "" {
		if edited {
			text = firstNonEmpty(editedText(edit), text)
		}
		return text + suffix, true
	}
	if contact := msg.GetContactMessage(); contact != nil {
		return r.contactCard(m, contact), true
	}
	if contacts := msg.GetContactsArrayMessage().GetContacts(); len(contacts) > 0 {
		cards := make([]string, 0, len(contacts))
		for _, contact := range contacts {
			cards = append(cards, r.contactCard(m, contact))
		}
		return strings.Join(cards, "\n"), true
	}
	if location := msg.GetLocationMessage(); location != nil {
		return fmt.Sprintf("%sLocation: https://maps.google.com/?q=%.6f,%.6f", lrm, location.GetDegreesLatitude(), location.GetDegreesLongitude()), true
	}
	if msg.GetLiveLocationMessage() != nil {
		return lrm + "Live location shared", true
	}
	if poll := pollOf(msg); poll != nil {
		lines := []string{lrm + "POLL:", poll.GetName()}
		for _, option := range poll.GetOptions() {
			lines = append(lines, lrm+"OPTION: "+option.GetOptionName())
		}
		return strings.Join(lines, "\n"), true
	}
	if text := businessText(msg); text != "" {
		return text + suffix, true
	}
	return "", false
}

func pollOf(msg *waE2E.Message) *waE2E.PollCreationMessage {
	for _, poll := range []*waE2E.PollCreationMessage{
		msg.GetPollCreationMessage(),
		msg.GetPollCreationMessageV2(),
		msg.GetPollCreationMessageV3(),
	} {
		if poll != nil {
			return poll
		}
	}
	return nil
}

func businessText(msg *waE2E.Message) string {
	return firstNonEmpty(
		msg.GetTemplateMessage().GetHydratedTemplate().GetHydratedContentText(),
		msg.GetTemplateMessage().GetHydratedFourRowTemplate().GetHydratedContentText(),
		msg.GetButtonsMessage().GetContentText(),
		msg.GetListMessage().GetDescription(),
		msg.GetInteractiveMessage().GetBody().GetText(),
		msg.GetButtonsResponseMessage().GetSelectedDisplayText(),
		msg.GetListResponseMessage().GetTitle(),
		msg.GetTemplateButtonReplyMessage().GetSelectedDisplayText(),
		msg.GetInteractiveResponseMessage().GetBody().GetText(),
	)
}

func (r *chatRenderer) mediaBody(m storedMessage, media mediaPart, caption string) string {
	var body string
	if r.skipMedia {
		body = lrm + media.kind.omitted
	} else if m.MediaState == mediaDone && m.MediaFile != "" {
		name := r.nextAttachmentName(media.ext)
		r.out.Attachments = append(r.out.Attachments, attachmentFile{
			Name:     name,
			Source:   filepath.Join(r.mediaDir, m.MediaFile),
			Modified: m.Timestamp,
		})
		body = lrm + "<attached: " + name + ">"
		if media.kind == kindDocument && media.fileName != "" {
			body = media.fileName + " " + body
		}
		if media.kind == kindAudio {
			if text, ok := readTranscript(filepath.Join(r.mediaDir, m.MediaFile)); ok && text != "" {
				body += "\n" + transcriptPrefix + text
			}
		}
	} else {
		body = lrm + media.kind.omitted
		r.out.Missing = append(r.out.Missing, missingAttachment{
			When:   m.Timestamp,
			Kind:   strings.TrimSuffix(media.kind.omitted, " omitted"),
			Reason: missingReason(m),
		})
	}
	if caption != "" {
		body += "\n" + caption
	}
	return body
}

func missingReason(m storedMessage) string {
	switch m.MediaState {
	case mediaUnavailable:
		return reasonNotOnPhone
	case mediaFailed:
		return firstNonEmpty(m.MediaError, "download failed")
	}
	return reasonNotDownloaded
}

func (r *chatRenderer) nextAttachmentName(ext string) string {
	r.seq++
	return fmt.Sprintf("%08d.%s", r.seq, sanitizeExt(ext))
}

func sanitizeExt(ext string) string {
	var b strings.Builder
	for _, c := range strings.ToLower(ext) {
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') {
			b.WriteRune(c)
		}
	}
	if b.Len() == 0 || b.Len() > 10 {
		return "bin"
	}
	return b.String()
}

func (r *chatRenderer) contactCard(m storedMessage, contact *waE2E.ContactMessage) string {
	if contact.GetVcard() == "" {
		return lrm + "Contact card omitted"
	}
	name := r.nextAttachmentName("vcf")
	r.out.Attachments = append(r.out.Attachments, attachmentFile{
		Name:     name,
		Data:     []byte(contact.GetVcard()),
		Modified: m.Timestamp,
	})
	return firstNonEmpty(contact.GetDisplayName(), "Contact") + ".vcf " + lrm + "<attached: " + name + ">"
}

func (r *chatRenderer) systemText(m storedMessage) (string, bool) {
	actor := ""
	switch {
	case m.FromMe:
		actor = "You"
	case m.SenderJID != "":
		actor = r.names.sender(m)
	}
	first := ""
	if len(m.StubParams) > 0 {
		first = m.StubParams[0]
	}
	people := r.peopleList(m.StubParams)
	var text string
	switch waWeb.WebMessageInfo_StubType(m.StubType) {
	case waWeb.WebMessageInfo_GROUP_CREATE:
		text = withActor(actor, "created group “"+first+"”", "Group “"+first+"” was created")
	case waWeb.WebMessageInfo_GROUP_CHANGE_SUBJECT:
		text = withActor(actor, "changed the group name to “"+first+"”", "The group name was changed to “"+first+"”")
	case waWeb.WebMessageInfo_GROUP_CHANGE_DESCRIPTION:
		text = withActor(actor, "changed the group description", "The group description was changed")
	case waWeb.WebMessageInfo_GROUP_CHANGE_ICON:
		text = withActor(actor, "changed this group's icon", "The group icon was changed")
	case waWeb.WebMessageInfo_GROUP_PARTICIPANT_ADD:
		if people == "" {
			return "", false
		}
		text = withActor(actor, "added "+people, people+" joined")
	case waWeb.WebMessageInfo_GROUP_PARTICIPANT_REMOVE:
		if people == "" {
			return "", false
		}
		text = withActor(actor, "removed "+people, people+" was removed")
	case waWeb.WebMessageInfo_GROUP_PARTICIPANT_LEAVE:
		if who := firstNonEmpty(people, actor); who != "" {
			text = who + " left"
		} else {
			return "", false
		}
	case waWeb.WebMessageInfo_GROUP_PARTICIPANT_INVITE,
		waWeb.WebMessageInfo_GROUP_PARTICIPANT_LINKED_GROUP_JOIN,
		waWeb.WebMessageInfo_GROUP_PARTICIPANT_ADD_REQUEST_JOIN:
		if who := firstNonEmpty(people, actor); who != "" {
			text = who + " joined using this group's invite link"
		} else {
			return "", false
		}
	case waWeb.WebMessageInfo_GROUP_PARTICIPANT_PROMOTE:
		if people == "" {
			return "", false
		}
		text = withActor(actor, "made "+people+" an admin", people+" is now an admin")
	case waWeb.WebMessageInfo_GROUP_PARTICIPANT_DEMOTE:
		if people == "" {
			return "", false
		}
		text = people + " is no longer an admin"
	case waWeb.WebMessageInfo_GROUP_PARTICIPANT_CHANGE_NUMBER:
		who := firstNonEmpty(r.peopleList(m.StubParams[:min(1, len(m.StubParams))]), actor)
		if who == "" {
			return "", false
		}
		text = who + " changed their phone number to a new number"
	case waWeb.WebMessageInfo_CALL_MISSED_VOICE, waWeb.WebMessageInfo_CALL_MISSED_GROUP_VOICE:
		text = "Missed voice call"
	case waWeb.WebMessageInfo_CALL_MISSED_VIDEO, waWeb.WebMessageInfo_CALL_MISSED_GROUP_VIDEO:
		text = "Missed video call"
	case waWeb.WebMessageInfo_CIPHERTEXT:
		text = "Waiting for this message. This may take a while."
	default:
		return "", false
	}
	return lrm + text, true
}

func withActor(actor, action, passive string) string {
	if actor == "" {
		return passive
	}
	return actor + " " + action
}

func (r *chatRenderer) peopleList(params []string) string {
	var names []string
	for _, param := range params {
		if !strings.Contains(param, "@") {
			continue
		}
		names = append(names, r.names.person(param))
	}
	switch len(names) {
	case 0:
		return ""
	case 1:
		return names[0]
	}
	return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
}

var reservedWindowsNames = map[string]bool{
	"CON": true, "PRN": true, "AUX": true, "NUL": true,
	"COM1": true, "COM2": true, "COM3": true, "COM4": true, "COM5": true, "COM6": true, "COM7": true, "COM8": true, "COM9": true,
	"LPT1": true, "LPT2": true, "LPT3": true, "LPT4": true, "LPT5": true, "LPT6": true, "LPT7": true, "LPT8": true, "LPT9": true,
}

func sanitizeFileName(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case r < 32 || r == 127:
			continue
		case strings.ContainsRune(`/\:*?"<>|`, r):
			b.WriteRune('_')
		default:
			b.WriteRune(r)
		}
	}
	clean := strings.Trim(b.String(), " .")
	if runes := []rune(clean); len(runes) > 120 {
		ext := filepath.Ext(clean)
		if len([]rune(ext)) > 10 {
			ext = ""
		}
		clean = strings.TrimRight(string(runes[:120-len([]rune(ext))]), " .") + ext
	}
	if clean == "" {
		return "Untitled"
	}
	if reservedWindowsNames[strings.ToUpper(strings.TrimSuffix(clean, filepath.Ext(clean)))] {
		clean = "_" + clean
	}
	return clean
}

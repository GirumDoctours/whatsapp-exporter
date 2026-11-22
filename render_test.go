package main

import (
	"strings"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/proto/waWeb"
	"google.golang.org/protobuf/proto"
)

type fakeNamer struct{}

func (fakeNamer) sender(m storedMessage) string {
	if m.FromMe {
		return "Northwind"
	}
	return map[string]string{
		"905551112233@s.whatsapp.net": "Ayşe Yılmaz",
		"447700900123@s.whatsapp.net": "+447700900123",
	}[m.SenderJID]
}

func (fakeNamer) person(jid string) string {
	return map[string]string{
		"905551112233@s.whatsapp.net": "Ayşe Yılmaz",
		"447700900123@s.whatsapp.net": "+447700900123",
	}[jid]
}

var testZone = time.FixedZone("TRT", 3*60*60)

func at(minute int) time.Time {
	return time.Date(2024, 9, 5, 8, minute, 50, 0, time.UTC)
}

func textMessage(id string, minute int, fromMe bool, text string) storedMessage {
	return storedMessage{
		ChatJID:   "905551112233@s.whatsapp.net",
		ID:        id,
		SenderJID: "905551112233@s.whatsapp.net",
		FromMe:    fromMe,
		Timestamp: at(minute),
		Message:   &waE2E.Message{Conversation: proto.String(text)},
	}
}

func TestRenderIndividualChatLikeIPhone(t *testing.T) {
	image := storedMessage{
		ChatJID:    "905551112233@s.whatsapp.net",
		ID:         "IMG1",
		SenderJID:  "905551112233@s.whatsapp.net",
		Timestamp:  at(3),
		MediaState: mediaDone,
		MediaFile:  "abc.jpg",
		Message: &waE2E.Message{ImageMessage: &waE2E.ImageMessage{
			Mimetype: proto.String("image/jpeg"),
			Caption:  proto.String("Before photo"),
		}},
	}
	lostVideo := storedMessage{
		ChatJID:    "905551112233@s.whatsapp.net",
		ID:         "VID1",
		SenderJID:  "905551112233@s.whatsapp.net",
		Timestamp:  at(4),
		MediaState: mediaUnavailable,
		Message:    &waE2E.Message{VideoMessage: &waE2E.VideoMessage{Mimetype: proto.String("video/mp4")}},
	}
	document := storedMessage{
		ChatJID:    "905551112233@s.whatsapp.net",
		ID:         "DOC1",
		FromMe:     true,
		Timestamp:  at(5),
		MediaState: mediaDone,
		MediaFile:  "def.pdf",
		Message: &waE2E.Message{DocumentMessage: &waE2E.DocumentMessage{
			Mimetype: proto.String("application/pdf"),
			FileName: proto.String("Treatment plan: final.pdf"),
		}},
	}
	edit := storedMessage{
		ChatJID:   "905551112233@s.whatsapp.net",
		ID:        "EDIT1",
		FromMe:    true,
		Timestamp: at(7),
		Message: &waE2E.Message{ProtocolMessage: &waE2E.ProtocolMessage{
			Type:          waE2E.ProtocolMessage_MESSAGE_EDIT.Enum(),
			Key:           &waCommon.MessageKey{ID: proto.String("TXT2")},
			EditedMessage: &waE2E.Message{Conversation: proto.String("See you at 10:30")},
		}},
	}
	revoke := storedMessage{
		ChatJID:   "905551112233@s.whatsapp.net",
		ID:        "DEL1",
		SenderJID: "905551112233@s.whatsapp.net",
		Timestamp: at(9),
		Message: &waE2E.Message{ProtocolMessage: &waE2E.ProtocolMessage{
			Type: waE2E.ProtocolMessage_REVOKE.Enum(),
			Key:  &waCommon.MessageKey{ID: proto.String("TXT3")},
		}},
	}
	reaction := storedMessage{
		ChatJID:   "905551112233@s.whatsapp.net",
		ID:        "REACT1",
		SenderJID: "905551112233@s.whatsapp.net",
		Timestamp: at(10),
		Message:   &waE2E.Message{ReactionMessage: &waE2E.ReactionMessage{Text: proto.String("👍")}},
	}
	location := storedMessage{
		ChatJID:   "905551112233@s.whatsapp.net",
		ID:        "LOC1",
		FromMe:    true,
		Timestamp: at(11),
		Message: &waE2E.Message{LocationMessage: &waE2E.LocationMessage{
			DegreesLatitude:  proto.Float64(41.0082),
			DegreesLongitude: proto.Float64(28.9784),
		}},
	}
	contact := storedMessage{
		ChatJID:   "905551112233@s.whatsapp.net",
		ID:        "VCF1",
		FromMe:    true,
		Timestamp: at(12),
		Message: &waE2E.Message{ContactMessage: &waE2E.ContactMessage{
			DisplayName: proto.String("Clinic Desk"),
			Vcard:       proto.String("BEGIN:VCARD\nVERSION:3.0\nFN:Clinic Desk\nEND:VCARD"),
		}},
	}
	msgs := []storedMessage{
		textMessage("TXT1", 1, false, "Hello, I'd like a consultation"),
		textMessage("TXT2", 2, true, "See you at 10"),
		image,
		lostVideo,
		document,
		textMessage("TXT3", 6, false, "oops wrong chat"),
		edit,
		revoke,
		reaction,
		location,
		contact,
		textMessage("TXT4", 13, false, "Line one\nLine two"),
	}

	got := renderChat("Ayşe Yılmaz", msgs, fakeNamer{}, "/media", testZone)

	want := strings.Join([]string{
		"[05/09/2024, 11:01:50] Ayşe Yılmaz: \u200eMessages and calls are end-to-end encrypted. Only people in this chat can read, listen to, or share them.",
		"[05/09/2024, 11:01:50] Ayşe Yılmaz: Hello, I'd like a consultation",
		"[05/09/2024, 11:02:50] Northwind: See you at 10:30 \u200e<This message was edited>",
		"[05/09/2024, 11:03:50] Ayşe Yılmaz: \u200e<attached: 00000001.jpg>",
		"Before photo",
		"[05/09/2024, 11:04:50] Ayşe Yılmaz: \u200evideo omitted",
		"[05/09/2024, 11:05:50] Northwind: Treatment plan: final.pdf \u200e<attached: 00000002.pdf>",
		"[05/09/2024, 11:06:50] Ayşe Yılmaz: \u200eThis message was deleted.",
		"[05/09/2024, 11:11:50] Northwind: \u200eLocation: https://maps.google.com/?q=41.008200,28.978400",
		"[05/09/2024, 11:12:50] Northwind: Clinic Desk.vcf \u200e<attached: 00000003.vcf>",
		"[05/09/2024, 11:13:50] Ayşe Yılmaz: Line one",
		"Line two",
		"",
	}, "\r\n")
	if got.Text != want {
		t.Fatalf("chat text mismatch\n--- got ---\n%s\n--- want ---\n%s", got.Text, want)
	}
	if got.Messages != 9 {
		t.Errorf("messages = %d, want 9", got.Messages)
	}
	if len(got.Attachments) != 3 {
		t.Fatalf("attachments = %d, want 3", len(got.Attachments))
	}
	if got.Attachments[0].Source != "/media/abc.jpg" {
		t.Errorf("first attachment source = %q", got.Attachments[0].Source)
	}
	if string(got.Attachments[2].Data) == "" {
		t.Errorf("contact card has no vCard data")
	}
	if len(got.Missing) != 1 || got.Missing[0].Kind != "video" || got.Missing[0].Reason != reasonNotOnPhone {
		t.Errorf("missing = %+v", got.Missing)
	}
}

func TestRenderGroupSystemMessages(t *testing.T) {
	group := "120363000000000000@g.us"
	msgs := []storedMessage{
		{
			ChatJID: group, ID: "S1", FromMe: true, IsGroup: true, Timestamp: at(0),
			StubType: int32(waWeb.WebMessageInfo_GROUP_CREATE), StubParams: []string{"Patients Sept"},
		},
		{
			ChatJID: group, ID: "S2", FromMe: true, IsGroup: true, Timestamp: at(1),
			StubType:   int32(waWeb.WebMessageInfo_GROUP_PARTICIPANT_ADD),
			StubParams: []string{"905551112233@s.whatsapp.net", "447700900123@s.whatsapp.net"},
		},
		{
			ChatJID: group, ID: "M1", SenderJID: "447700900123@s.whatsapp.net", IsGroup: true, Timestamp: at(2),
			Message: &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{Text: proto.String("Thanks for adding me")}},
		},
		{
			ChatJID: group, ID: "S3", SenderJID: "905551112233@s.whatsapp.net", IsGroup: true, Timestamp: at(3),
			StubType:   int32(waWeb.WebMessageInfo_GROUP_PARTICIPANT_LEAVE),
			StubParams: []string{"905551112233@s.whatsapp.net"},
		},
		{
			ChatJID: group, ID: "S4", IsGroup: true, Timestamp: at(4),
			StubType: int32(waWeb.WebMessageInfo_CHANGE_EPHEMERAL_SETTING),
		},
	}

	got := renderChat("Patients Sept", msgs, fakeNamer{}, "/media", testZone)

	want := strings.Join([]string{
		"[05/09/2024, 11:00:50] Patients Sept: \u200eMessages and calls are end-to-end encrypted. Only people in this chat can read, listen to, or share them.",
		"[05/09/2024, 11:00:50] Patients Sept: \u200eYou created group “Patients Sept”",
		"[05/09/2024, 11:01:50] Patients Sept: \u200eYou added Ayşe Yılmaz and +447700900123",
		"[05/09/2024, 11:02:50] +447700900123: Thanks for adding me",
		"[05/09/2024, 11:03:50] Patients Sept: \u200eAyşe Yılmaz left",
		"",
	}, "\r\n")
	if got.Text != want {
		t.Fatalf("group text mismatch\n--- got ---\n%s\n--- want ---\n%s", got.Text, want)
	}
}

func TestRenderSkipsChatsWithNothingToShow(t *testing.T) {
	msgs := []storedMessage{{
		ChatJID: "905551112233@s.whatsapp.net", ID: "R1", Timestamp: at(0),
		Message: &waE2E.Message{ReactionMessage: &waE2E.ReactionMessage{Text: proto.String("❤️")}},
	}}
	got := renderChat("Ayşe", msgs, fakeNamer{}, "/media", testZone)
	if got.Messages != 0 || got.Text != "" {
		t.Fatalf("expected empty render, got %d messages: %q", got.Messages, got.Text)
	}
}

func TestSanitizeFileName(t *testing.T) {
	cases := map[string]string{
		"Treatment plan: final.pdf": "Treatment plan_ final.pdf",
		"  ..hidden.. ":             "hidden",
		"CON":                       "_CON",
		"a/b\\c?.txt":               "a_b_c_.txt",
		"":                          "Untitled",
		"Ayşe 🌸":                    "Ayşe 🌸",
	}
	for input, want := range cases {
		if got := sanitizeFileName(input); got != want {
			t.Errorf("sanitizeFileName(%q) = %q, want %q", input, got, want)
		}
	}
	long := strings.Repeat("x", 200) + ".pdf"
	if got := sanitizeFileName(long); len([]rune(got)) != 120 || !strings.HasSuffix(got, ".pdf") {
		t.Errorf("long name not trimmed correctly: %d runes, %q", len([]rune(got)), got[len(got)-8:])
	}
}

func TestFormatCount(t *testing.T) {
	cases := map[int]string{0: "0", 999: "999", 1000: "1,000", 4735: "4,735", 1234567: "1,234,567", -1500: "-1,500"}
	for n, want := range cases {
		if got := formatCount(n); got != want {
			t.Errorf("formatCount(%d) = %q, want %q", n, got, want)
		}
	}
}

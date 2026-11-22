package main

import (
	"mime"
	"path/filepath"
	"strings"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
)

type mediaKind struct {
	label   string
	omitted string
}

var (
	kindPhoto    = mediaKind{label: "PHOTO", omitted: "image omitted"}
	kindVideo    = mediaKind{label: "VIDEO", omitted: "video omitted"}
	kindGIF      = mediaKind{label: "GIF", omitted: "GIF omitted"}
	kindAudio    = mediaKind{label: "AUDIO", omitted: "audio omitted"}
	kindSticker  = mediaKind{label: "STICKER", omitted: "sticker omitted"}
	kindDocument = mediaKind{label: "DOCUMENT", omitted: "document omitted"}
)

type mediaPart struct {
	part     whatsmeow.DownloadableMessage
	kind     mediaKind
	ext      string
	fileName string
	caption  string
}

var mimeExtensions = map[string]string{
	"image/jpeg":      "jpg",
	"image/png":       "png",
	"image/webp":      "webp",
	"image/gif":       "gif",
	"image/heic":      "heic",
	"video/mp4":       "mp4",
	"video/3gpp":      "3gp",
	"video/quicktime": "mov",
	"audio/ogg":       "opus",
	"audio/mp4":       "m4a",
	"audio/aac":       "aac",
	"audio/mpeg":      "mp3",
	"audio/amr":       "amr",
	"application/pdf": "pdf",
}

func contentOf(msg *waE2E.Message) *waE2E.Message {
	if inner := msg.GetDocumentWithCaptionMessage().GetMessage(); inner != nil {
		return inner
	}
	return msg
}

func findMedia(msg *waE2E.Message) (mediaPart, bool) {
	msg = contentOf(msg)
	if m := msg.GetImageMessage(); m != nil {
		return mediaPart{part: m, kind: kindPhoto, ext: extFor(m.GetMimetype(), "jpg"), caption: m.GetCaption()}, true
	}
	if m := msg.GetVideoMessage(); m != nil {
		kind := kindVideo
		if m.GetGifPlayback() {
			kind = kindGIF
		}
		return mediaPart{part: m, kind: kind, ext: extFor(m.GetMimetype(), "mp4"), caption: m.GetCaption()}, true
	}
	if m := msg.GetPtvMessage(); m != nil {
		return mediaPart{part: m, kind: kindVideo, ext: extFor(m.GetMimetype(), "mp4")}, true
	}
	if m := msg.GetAudioMessage(); m != nil {
		return mediaPart{part: m, kind: kindAudio, ext: extFor(m.GetMimetype(), "opus")}, true
	}
	if m := msg.GetStickerMessage(); m != nil {
		return mediaPart{part: m, kind: kindSticker, ext: extFor(m.GetMimetype(), "webp")}, true
	}
	if m := msg.GetDocumentMessage(); m != nil {
		ext := strings.TrimPrefix(filepath.Ext(m.GetFileName()), ".")
		if ext == "" {
			ext = extFor(m.GetMimetype(), "bin")
		}
		return mediaPart{part: m, kind: kindDocument, ext: ext, fileName: m.GetFileName(), caption: m.GetCaption()}, true
	}
	return mediaPart{}, false
}

func extFor(mimetype, fallback string) string {
	base := strings.ToLower(strings.TrimSpace(strings.SplitN(mimetype, ";", 2)[0]))
	if ext, ok := mimeExtensions[base]; ok {
		return ext
	}
	if exts, _ := mime.ExtensionsByType(base); len(exts) > 0 {
		return strings.TrimPrefix(exts[0], ".")
	}
	return fallback
}

func setDirectPath(part whatsmeow.DownloadableMessage, path string) bool {
	switch m := part.(type) {
	case *waE2E.ImageMessage:
		m.DirectPath = &path
	case *waE2E.VideoMessage:
		m.DirectPath = &path
	case *waE2E.AudioMessage:
		m.DirectPath = &path
	case *waE2E.StickerMessage:
		m.DirectPath = &path
	case *waE2E.DocumentMessage:
		m.DirectPath = &path
	default:
		return false
	}
	return true
}

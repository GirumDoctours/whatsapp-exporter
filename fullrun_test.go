package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/store"
	"google.golang.org/protobuf/proto"
)

func seededExporter(t *testing.T, chats int) *exporter {
	t.Helper()
	ctx := context.Background()
	e, err := openExporter(filepath.Join(t.TempDir(), "data", "export-1"))
	must(t, err)
	t.Cleanup(e.close)
	e.cli.Store.Contacts = &store.NoopStore{}
	e.cli.Store.LIDs = &store.NoopStore{}
	e.testMode, e.messagesOnly = true, true
	must(t, e.arc.setMeta(ctx, quickTestMeta, "true"))
	for i := range chats {
		jid := fmt.Sprintf("4477009001%02d@s.whatsapp.net", i)
		must(t, e.arc.upsertChat(ctx, chatRecord{JID: jid}))
		_, err := e.arc.insertMessages(ctx, []storedMessage{{
			ChatJID: jid, ID: fmt.Sprintf("M%d", i), SenderJID: jid, Timestamp: at(i),
			Message: &waE2E.Message{Conversation: proto.String("hello")},
		}})
		must(t, err)
	}
	return e
}

func finishOneExport(t *testing.T, e *exporter, term *terminal, out string) map[string]string {
	t.Helper()
	ctx := context.Background()
	if e.testMode {
		must(t, e.selectTestChats(ctx))
	}
	plan, err := e.prepareFolder(ctx, "15550001111")
	must(t, err)
	must(t, e.completeExport(ctx, term, plan, out, "15550001111", time.Now()))
	return readZipFiles(t, e.savedZip)
}

func chatCount(files map[string]string) int {
	n := 0
	for name := range files {
		if strings.HasSuffix(name, "/_chat.txt") {
			n++
		}
	}
	return n
}

func TestQuickTestContinuesIntoFullRun(t *testing.T) {
	for _, redact := range []bool{false, true} {
		t.Run(fmt.Sprintf("redact %v", redact), func(t *testing.T) {
			ctx := context.Background()
			e := seededExporter(t, testChatLimit+3)
			e.redact = redact
			e.notes = []string{"left over from the test"}
			term, screen := quietTerminal()
			out := t.TempDir()

			if n := chatCount(finishOneExport(t, e, term, out)); n != testChatLimit {
				t.Fatalf("quick test saved %d chats, want %d", n, testChatLimit)
			}
			testZip := e.savedZip
			if e.closed || !strings.Contains(filepath.Base(testZip), "-test") {
				t.Fatalf("after the test: closed %v, zip %s", e.closed, testZip)
			}
			if strings.Contains(screen.String(), "Unlinked") || strings.Contains(screen.String(), "Original messages and files deleted") {
				t.Fatalf("nothing is unlinked or fully deleted before the question is answered:\n%s", screen.String())
			}

			e.switchToFullRun(ctx)
			if found := resumesAsQuickTest(t, e); found != "false" {
				t.Fatalf("an interrupted full run would resume as quick test %q", found)
			}
			if n := chatCount(finishOneExport(t, e, term, out)); n != testChatLimit+3 {
				t.Fatalf("full run saved %d chats, want %d", n, testChatLimit+3)
			}
			if e.savedZip == testZip || strings.Contains(filepath.Base(e.savedZip), "-test") {
				t.Fatalf("full run zip %s", e.savedZip)
			}
			if _, err := os.Stat(testZip); err != nil {
				t.Errorf("the test ZIP should stay: %v", err)
			}
			if _, err := os.Stat(e.dir); !os.IsNotExist(err) || !e.closed || len(e.notes) != 0 {
				t.Errorf("after the full run: folder error %v, closed %v, notes %v", err, e.closed, e.notes)
			}
			if got := strings.Contains(screen.String(), "Original messages and files deleted"); got != redact {
				t.Errorf("deletion confirmed %v, want %v", got, redact)
			}
		})
	}
}

func resumesAsQuickTest(t *testing.T, e *exporter) string {
	t.Helper()
	return e.arc.meta(context.Background(), quickTestMeta)
}

func TestDecliningFullRunCleansUp(t *testing.T) {
	ctx := context.Background()
	e := seededExporter(t, 2)
	term, _ := quietTerminal()
	finishOneExport(t, e, term, t.TempDir())
	if e.wantsFullRun(ctx, term) {
		t.Fatal("a window that can't answer must not start a full run")
	}
	e.cleanUp(ctx, term)
	if _, err := os.Stat(e.dir); !os.IsNotExist(err) {
		t.Errorf("working folder left behind: %v", err)
	}
	if _, err := os.Stat(e.savedZip); err != nil {
		t.Errorf("the test ZIP should stay: %v", err)
	}
}

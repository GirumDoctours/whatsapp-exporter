package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"
	_ "modernc.org/sqlite"
)

const (
	mediaNone = iota
	mediaPending
	mediaDone
	mediaUnavailable
	mediaFailed
)

const archiveSchema = `
CREATE TABLE IF NOT EXISTS meta (
	key   TEXT PRIMARY KEY,
	value TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS chats (
	jid            TEXT PRIMARY KEY,
	name           TEXT NOT NULL DEFAULT '',
	pn_jid         TEXT NOT NULL DEFAULT '',
	lid_jid        TEXT NOT NULL DEFAULT '',
	archived       INTEGER NOT NULL DEFAULT 0,
	end_of_history INTEGER NOT NULL DEFAULT 0,
	backfill_done  INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS messages (
	chat_jid       TEXT NOT NULL,
	id             TEXT NOT NULL,
	sender_jid     TEXT NOT NULL DEFAULT '',
	sender_alt_jid TEXT NOT NULL DEFAULT '',
	from_me        INTEGER NOT NULL DEFAULT 0,
	is_group       INTEGER NOT NULL DEFAULT 0,
	ts             INTEGER NOT NULL,
	push_name      TEXT NOT NULL DEFAULT '',
	stub_type      INTEGER NOT NULL DEFAULT 0,
	stub_params    TEXT NOT NULL DEFAULT '',
	message        BLOB,
	media_state    INTEGER NOT NULL DEFAULT 0,
	media_file     TEXT NOT NULL DEFAULT '',
	media_error    TEXT NOT NULL DEFAULT '',
	PRIMARY KEY (chat_jid, id)
);
CREATE INDEX IF NOT EXISTS messages_chat_ts ON messages (chat_jid, ts);
CREATE INDEX IF NOT EXISTS messages_media_state ON messages (media_state);
`

const insertMessageSQL = `
INSERT INTO messages (chat_jid, id, sender_jid, sender_alt_jid, from_me, is_group, ts, push_name, stub_type, stub_params, message, media_state)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (chat_jid, id) DO UPDATE SET
	message = excluded.message,
	media_state = excluded.media_state,
	stub_type = excluded.stub_type,
	stub_params = excluded.stub_params
WHERE messages.message IS NULL AND excluded.message IS NOT NULL
`

const upsertChatSQL = `
INSERT INTO chats (jid, name, pn_jid, lid_jid, archived, end_of_history)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT (jid) DO UPDATE SET
	name = CASE WHEN excluded.name != '' THEN excluded.name ELSE chats.name END,
	pn_jid = CASE WHEN excluded.pn_jid != '' THEN excluded.pn_jid ELSE chats.pn_jid END,
	lid_jid = CASE WHEN excluded.lid_jid != '' THEN excluded.lid_jid ELSE chats.lid_jid END,
	archived = MAX(chats.archived, excluded.archived),
	end_of_history = MAX(chats.end_of_history, excluded.end_of_history)
`

const selectMessageColumns = `chat_jid, id, sender_jid, sender_alt_jid, from_me, is_group, ts, push_name, stub_type, stub_params, message, media_state, media_file, media_error`

type archive struct {
	db *sql.DB
}

type chatRecord struct {
	JID          string
	Name         string
	PN           string
	LID          string
	Archived     bool
	EndOfHistory bool
	LastMessage  time.Time
	MessageCount int
}

type storedMessage struct {
	ChatJID      string
	ID           string
	SenderJID    string
	SenderAltJID string
	FromMe       bool
	IsGroup      bool
	Timestamp    time.Time
	PushName     string
	StubType     int32
	StubParams   []string
	Message      *waE2E.Message
	MediaState   int
	MediaFile    string
	MediaError   string
}

func (m storedMessage) messageInfo() types.MessageInfo {
	chat, _ := types.ParseJID(m.ChatJID)
	sender, _ := types.ParseJID(m.SenderJID)
	return types.MessageInfo{
		MessageSource: types.MessageSource{
			Chat:     chat,
			Sender:   sender,
			IsFromMe: m.FromMe,
			IsGroup:  m.IsGroup,
		},
		ID:        m.ID,
		Timestamp: m.Timestamp,
	}
}

func sqliteDSN(path string) string {
	return path + "?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(15000)&_pragma=synchronous(NORMAL)"
}

func openArchive(path string) (*archive, error) {
	db, err := sql.Open("sqlite", sqliteDSN(path))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(archiveSchema); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &archive{db: db}, nil
}

func (a *archive) close() error {
	return a.db.Close()
}

func (a *archive) meta(ctx context.Context, key string) string {
	var value string
	err := a.db.QueryRowContext(ctx, `SELECT value FROM meta WHERE key = ?`, key).Scan(&value)
	if err != nil {
		return ""
	}
	return value
}

func (a *archive) setMeta(ctx context.Context, key, value string) error {
	_, err := a.db.ExecContext(ctx, `INSERT INTO meta (key, value) VALUES (?, ?) ON CONFLICT (key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

func (a *archive) upsertChat(ctx context.Context, chat chatRecord) error {
	_, err := a.db.ExecContext(ctx, upsertChatSQL, chat.JID, chat.Name, chat.PN, chat.LID, chat.Archived, chat.EndOfHistory)
	return err
}

func (a *archive) setChatName(ctx context.Context, jid, name string) error {
	_, err := a.db.ExecContext(ctx, `INSERT INTO chats (jid, name) VALUES (?, ?) ON CONFLICT (jid) DO UPDATE SET name = excluded.name`, jid, name)
	return err
}

func (a *archive) ensureChat(ctx context.Context, jid string) error {
	_, err := a.db.ExecContext(ctx, `INSERT OR IGNORE INTO chats (jid) VALUES (?)`, jid)
	return err
}

func (a *archive) insertMessages(ctx context.Context, msgs []storedMessage) (int, error) {
	if len(msgs) == 0 {
		return 0, nil
	}
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	stmt, err := tx.PrepareContext(ctx, insertMessageSQL)
	if err != nil {
		return 0, err
	}
	defer stmt.Close()
	added := 0
	for _, m := range msgs {
		var raw []byte
		if m.Message != nil {
			if raw, err = proto.Marshal(m.Message); err != nil {
				return 0, err
			}
		}
		params := ""
		if len(m.StubParams) > 0 {
			encoded, _ := json.Marshal(m.StubParams)
			params = string(encoded)
		}
		res, err := stmt.ExecContext(ctx,
			m.ChatJID, m.ID, m.SenderJID, m.SenderAltJID, m.FromMe, m.IsGroup, m.Timestamp.Unix(),
			m.PushName, m.StubType, params, raw, m.MediaState,
		)
		if err != nil {
			return 0, err
		}
		if n, err := res.RowsAffected(); err == nil {
			added += int(n)
		}
	}
	return added, tx.Commit()
}

func (a *archive) counts(ctx context.Context) (messages, chats int) {
	_ = a.db.QueryRowContext(ctx, `SELECT COUNT(*), COUNT(DISTINCT chat_jid) FROM messages`).Scan(&messages, &chats)
	return
}

func (a *archive) chatsNeedingBackfill(ctx context.Context) ([]string, error) {
	rows, err := a.db.QueryContext(ctx, `
		SELECT m.chat_jid FROM messages m
		LEFT JOIN chats c ON c.jid = m.chat_jid
		WHERE COALESCE(c.end_of_history, 0) = 0 AND COALESCE(c.backfill_done, 0) = 0
		GROUP BY m.chat_jid
		ORDER BY MAX(m.ts) DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var chats []string
	for rows.Next() {
		var jid string
		if err := rows.Scan(&jid); err != nil {
			return nil, err
		}
		chats = append(chats, jid)
	}
	return chats, rows.Err()
}

func (a *archive) markBackfillDone(ctx context.Context, jid string) error {
	_, err := a.db.ExecContext(ctx, `INSERT INTO chats (jid, backfill_done) VALUES (?, 1) ON CONFLICT (jid) DO UPDATE SET backfill_done = 1`, jid)
	return err
}

func (a *archive) oldestMessage(ctx context.Context, chatJID string) (*types.MessageInfo, error) {
	var id string
	var fromMe bool
	var ts int64
	err := a.db.QueryRowContext(ctx, `SELECT id, from_me, ts FROM messages WHERE chat_jid = ? ORDER BY ts ASC, rowid ASC LIMIT 1`, chatJID).Scan(&id, &fromMe, &ts)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	chat, err := types.ParseJID(chatJID)
	if err != nil {
		return nil, err
	}
	return &types.MessageInfo{
		MessageSource: types.MessageSource{Chat: chat, IsFromMe: fromMe, IsGroup: chat.Server == types.GroupServer},
		ID:            id,
		Timestamp:     time.Unix(ts, 0),
	}, nil
}

func (a *archive) mediaQueue(ctx context.Context) ([]storedMessage, error) {
	return a.queryMessages(ctx, `SELECT `+selectMessageColumns+` FROM messages WHERE media_state IN (?, ?) ORDER BY ts DESC`, mediaPending, mediaFailed)
}

func (a *archive) setMedia(ctx context.Context, chatJID, id string, state int, file, errText string) error {
	_, err := a.db.ExecContext(ctx, `UPDATE messages SET media_state = ?, media_file = ?, media_error = ? WHERE chat_jid = ? AND id = ?`, state, file, errText, chatJID, id)
	return err
}

func (a *archive) listChats(ctx context.Context) ([]chatRecord, error) {
	rows, err := a.db.QueryContext(ctx, `
		SELECT m.chat_jid, COALESCE(c.name, ''), COALESCE(c.pn_jid, ''), COALESCE(c.lid_jid, ''),
			COALESCE(c.archived, 0), COALESCE(c.end_of_history, 0), MAX(m.ts), COUNT(*)
		FROM messages m
		LEFT JOIN chats c ON c.jid = m.chat_jid
		GROUP BY m.chat_jid`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var chats []chatRecord
	for rows.Next() {
		var c chatRecord
		var last int64
		if err := rows.Scan(&c.JID, &c.Name, &c.PN, &c.LID, &c.Archived, &c.EndOfHistory, &last, &c.MessageCount); err != nil {
			return nil, err
		}
		c.LastMessage = time.Unix(last, 0)
		chats = append(chats, c)
	}
	return chats, rows.Err()
}

func (a *archive) messagesFor(ctx context.Context, chatJIDs []string) ([]storedMessage, error) {
	if len(chatJIDs) == 0 {
		return nil, nil
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(chatJIDs)), ",")
	args := make([]any, len(chatJIDs))
	for i, jid := range chatJIDs {
		args[i] = jid
	}
	return a.queryMessages(ctx, `SELECT `+selectMessageColumns+` FROM messages WHERE chat_jid IN (`+placeholders+`) ORDER BY ts ASC, rowid ASC`, args...)
}

func (a *archive) queryMessages(ctx context.Context, query string, args ...any) ([]storedMessage, error) {
	rows, err := a.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var msgs []storedMessage
	for rows.Next() {
		var m storedMessage
		var ts int64
		var params string
		var raw []byte
		if err := rows.Scan(&m.ChatJID, &m.ID, &m.SenderJID, &m.SenderAltJID, &m.FromMe, &m.IsGroup, &ts, &m.PushName,
			&m.StubType, &params, &raw, &m.MediaState, &m.MediaFile, &m.MediaError); err != nil {
			return nil, err
		}
		m.Timestamp = time.Unix(ts, 0)
		if params != "" {
			_ = json.Unmarshal([]byte(params), &m.StubParams)
		}
		if len(raw) > 0 {
			m.Message = &waE2E.Message{}
			if err := proto.Unmarshal(raw, m.Message); err != nil {
				m.Message = nil
			}
		}
		msgs = append(msgs, m)
	}
	return msgs, rows.Err()
}

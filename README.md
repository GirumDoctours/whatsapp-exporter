# WhatsApp Exporter

Saves every chat, photo and file from your WhatsApp or WhatsApp Business account into one ZIP on your computer, laid out like WhatsApp's own "Export chat". It works on Mac and Windows and runs on your computer. Optionally it saves a redacted version instead, with personal details replaced by template tags such as `{{PATIENT_FIRST_NAME}}`.

## Run it

1. Open the command window and paste one line, then press Enter:
   - **Mac:** open **Terminal** (press ⌘ Space, type `Terminal`, press Enter) and paste:

     ```bash
     /bin/bash -c "$(curl -fsSL https://gist.githubusercontent.com/GirumDoctours/74929d87bb6c220c23bea4d9d1b37377/raw/install.sh)"
     ```

   - **Windows:** open **PowerShell** (click Start, type `PowerShell`, press Enter) and paste:

     ```powershell
     irm https://gist.githubusercontent.com/GirumDoctours/74929d87bb6c220c23bea4d9d1b37377/raw/install.ps1 | iex
     ```

2. Use the arrow keys and Enter (or Space) to choose:
   - **Run a quick test**: your 10 latest chats, in about 3 minutes. When it's done, it asks whether to do a full run now. **Yes** carries on with the same choices, without linking your phone again. **No** unlinks and exits.
   - **Export everything**: every chat, photo and file. About 25 minutes for a busy account, longer for very large ones.
3. Choose **Messages only** (the default) or **Messages and media**. Messages only skips photos, videos, voice notes and files, so it's much faster; the chat text shows "image omitted" where media was, and the ZIP name ends in `-messages-only`.
   With media, voice notes can also be turned into text: each one is sent to Deepgram's Nova-3 speech-to-text with your own Deepgram key (console.deepgram.com; new accounts get free credit, then roughly 26 cents per hour of audio). The transcript appears under the voice note in `_chat.txt`. In the redacted copy the transcript is redacted like any message, and the audio itself is replaced. Press Enter without a key to skip this.
4. Choose **Redact PII, then zip** or **Just zip**. Redacting saves only a redacted ZIP, with names, phone numbers, emails, handles, ages, addresses, ID numbers, bank details and ID document photos replaced; the original messages and files are deleted. It uses Claude, Anthropic's AI, with your own API key (create one at platform.claude.com under Settings, then API keys). The text of your chats is sent to Claude with known names, numbers and emails already hidden; photos and documents never leave your computer (voice notes go to Deepgram only if you add a Deepgram key). Press Enter without a key to use local checks only, which miss more.
5. Link your phone. In WhatsApp, open **Settings → Linked Devices → Link a Device** (on Android: **⋮ → Linked devices → Link a device**) and scan the QR code. If the window is too small for the code, maximize it. If scanning doesn't work, choose the pairing code option and type the 8-character code on your phone.
6. Keep WhatsApp open on your phone, with the screen unlocked, until it finishes. If the phone locks or WhatsApp is closed, the progress bar shows "Paused: waiting for your phone" and carries on by itself when you open WhatsApp again.

When it's done, a summary shows the totals and the ZIP is saved on your **Desktop**, then shown in Finder (Mac) or File Explorer (Windows). It's named after your number and the month, for example `15551234567-11-26.zip` (a quick test adds `-test`), or `WhatsApp-11-26-redacted.zip` if you redact. On a Mac, the first time, macOS may ask whether Terminal can access your Desktop: click Allow. If the Desktop can't be used, the ZIP is saved in the **WhatsApp Exporter** folder in your home folder instead, and the summary says so. The exporter then (after a quick test, once you answer No to a full run) unlinks itself from your phone, deletes its working files so only the ZIP is left, and closes the window.

Each successful quick test deletes the ZIPs from earlier quick tests, so they don't pile up. Full exports are never deleted.

If it stops partway (the computer sleeps, Wi-Fi drops, you press Ctrl+C), paste the same line again and choose to continue. It remembers whether it was a quick test or a full export and picks up where it left off.

## What's in the ZIP

- One folder per chat: the contact's name (or phone number if they aren't saved) for one-to-one chats, and `GP-<group name>` for groups.
- Each folder holds `_chat.txt` and that chat's media. Media files are named only by the attachment ID used in the chat text, for example `00000001.jpg`, which `_chat.txt` refers to as `<attached: 00000001.jpg>`. For documents, the original file name appears in the chat text before the attachment.
- `index.csv`: every chat with its number, profile name, message counts and date range.
- `report.txt`: totals, plus a list of any media that couldn't be exported and why.

Individual, group and archived chats are included. Status updates, channels and broadcast lists are not.

## The redacted copy

If you choose to redact, the only file saved is a ZIP named like `WhatsApp-11-26-redacted.zip`. The original is never written as a ZIP, and the working files it came from are deleted once the redacted ZIP is saved. If the run is stopped or fails before that, the working files stay in the `WhatsApp Exporter Data` folder so it can be finished; open the exporter again to finish it. If any chat can't be redacted, nothing is saved. The redaction runs while the export is still downloading, ten chats at a time, so it adds little time.

- Every personal detail becomes a tag from a fixed dictionary (`redact_dictionary.json`), as specific as the chat allows: `{{PATIENT_SISTER_PHONE}}`, `{{CLINIC_ADDRESS}}`, `{{HOTEL_NAME}}`, `{{SURGERY_DATE}}`. The same person or value always gets the same tag within a chat, and a second person with the same role is numbered, for example `{{PATIENT_FRIEND_2_FIRST_NAME}}`.
- Sender names, folder names, `index.csv` and `report.txt` are tagged too.
- Local checks run first (known contacts, phone numbers, emails, links, card numbers, IBANs, ID numbers, ages, birth dates, postcodes). Claude Haiku then assigns each person a role, tags every chunk, and a second Haiku pass checks every tag and looks for anything missed.
- A final local step then rereads each chat on your computer, at no AI cost. It adds names from greetings, titles, relations and lists, organisation names, dates, file names and contact details, and spreads what it finds through that chat only. Words the chat uses as ordinary words, and place names, are left alone, and it can never undo an earlier replacement.
- A strict check then proves that each message changed only where a tag was inserted and that every tag is in the dictionary. A message that fails is rebuilt.
- Photos are read with the text recognition built into macOS and Windows. Passports, visas, ID cards and driving licences are replaced with a "Removed: ID document" image. Phone numbers, emails and names in screenshots are blacked out, and GPS locations are removed from photo files.
- Files fail closed. PDFs are replaced if they can't be read or look personal. Documents, spreadsheets, presentations and archives are always replaced, because they can't be checked. Voice notes are replaced. Videos and stickers are copied as they are, and the report counts them.
- Every link is replaced by default, with a specific tag where the kind is known (event, video, post, document, payment, meeting, maps, profile or invite link). WhatsApp's own notices (joins, adds, subject changes) are recognised so their wording isn't mistaken for names.
- The redacted ZIP includes `redaction-report.txt` with counts and confidence. It never includes original text.

Automatic redaction is careful but not perfect: check the redacted copy before sharing it. Videos aren't checked and are copied as they are.

## Good to know

- Mac: needs macOS 13 or newer, on Apple Silicon or Intel.
- Windows: needs 64-bit Windows 10 or 11 (Intel, AMD or ARM). The app isn't code-signed, so in rare cases antivirus software may flag it; it's safe to allow.
- History comes from your phone. Anything it no longer has (deleted messages, media it has cleared, history lost in a reinstall without a backup) can't be exported.
- Media older than a few weeks has to be re-sent by the phone, so it's slower. Files the phone no longer has are listed in `report.txt` as "no longer on the phone".
- This is an unofficial tool, not made by or affiliated with WhatsApp or Meta. It links to your account as a linked device, the same way WhatsApp Web does, using the [whatsmeow](https://github.com/tulir/whatsmeow) library. Unofficial clients are against WhatsApp's terms of service. Read-only use like this is low risk, but not zero.

## For developers

### Building

Requires Go 1.27+ and, for the Mac app, the Xcode command line tools. Build on a Mac:

```bash
./build.sh
```

The Mac build uses cgo for Apple's Vision text recognition, so `build.sh` compiles each architecture with clang and a macOS 13 deployment target. The Windows build stays pure Go and runs Windows' own OCR through a hidden PowerShell helper.

This produces `dist/WhatsApp Exporter` (a universal Mac app for Apple Silicon and Intel) and `dist/WhatsApp Exporter-windows-amd64.exe` and `-arm64.exe`. Run the tests with `go test ./...`. By default the ZIP goes to the Desktop (Windows asks the system for it, so a Desktop moved into OneDrive is found) and the working data goes next to the app. The `-dir` flag puts both in the given folder instead.

The Mac and Windows versions share all their code. The few differences live in `platform_darwin.go` and `platform_windows.go`: showing the ZIP in Finder or File Explorer, making the window big enough for the QR code, and plainer symbols in the older Windows console. The Mac app closes its own Terminal window at the end; on Windows, `install.ps1` closes PowerShell after a successful run.

For tests that call Claude or Deepgram, put `ANTHROPIC_API_KEY=…` and `DEEPGRAM_API_KEY=…` in a `.env` file in this folder (git ignores it; the app itself never reads it). `EXPORTER_AI_EVAL=1 go test -run TestLiveRedaction -v` redacts a small made-up chat with real Haiku. `EXPORTER_DICT_SCAN=<export folder> go test -run TestScanDictionary -timeout 2h` runs Haiku over an existing export to grow the tag dictionary. It writes `redact_dictionary.json` and a `dictionary-coverage.txt` report containing tag counts only. `-redact-folder <export folder>` redacts an existing export folder without a phone, for testing.

Start the app with `--debug` (for example `~/WhatsApp\ Exporter/WhatsApp\ Exporter --debug` on a Mac) to get a detailed log as `debug.txt` inside the ZIP: every phase with timings, each chat written and redacted, AI requests, retries and rate limits, photo checks and the ZIP steps. It's never saved after a successful redaction, because it can mention names and numbers. If a run fails, `debug.txt` is saved next to the app instead. Library debug detail (WhatsApp protocol frames) is added only with `WHATSAPP_EXPORTER_DEBUG=1`.

While running, the exporter keeps its login, a message database and downloaded media in a `WhatsApp Exporter Data` folder next to the app, so an interrupted export can resume. A log file for troubleshooting is in there too (`exporter.log`). Set `WHATSAPP_EXPORTER_DEBUG=1` for more detail. The whole folder is deleted once an export succeeds.

### Releasing

```bash
./release.sh
```

Downloads are served from an unlisted GitHub Gist (`74929d87bb6c220c23bea4d9d1b37377`), so they can be fetched by anyone with the link but don't show up in search. This source repository is private.

`release.sh` needs a clean working tree and the GitHub CLI signed in as the gist's owner. It regenerates `THIRD-PARTY-NOTICES.txt`, runs the tests, builds the Mac and Windows apps, and replaces the gist's contents with the compressed apps, both install scripts, this README, the licence files and `whatsapp-exporter-source.tar.gz` (the source of the exact commit being released). It then pushes `main` to this repository. The install lines always fetch whatever is in the gist; GitHub may take a few minutes to serve the new files.

### Licence

WhatsApp Exporter is free software under the GNU General Public License v3; see [LICENSE](LICENSE). It includes third-party libraries under their own licences, listed in [THIRD-PARTY-NOTICES.txt](THIRD-PARTY-NOTICES.txt). The complete source for every release is published next to the downloads as `whatsapp-exporter-source.tar.gz`.

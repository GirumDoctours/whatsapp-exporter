# WhatsApp Exporter

Saves every chat, photo and file from your WhatsApp or WhatsApp Business account into one ZIP on your computer, laid out like WhatsApp's own "Export chat". It works on Mac and Windows and runs entirely on your computer: nothing is uploaded anywhere.

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
   - **Run a quick test**: your 10 latest chats, in about 3 minutes.
   - **Export everything**: every chat, photo and file. About 25 minutes for a busy account, longer for very large ones.
3. Link your phone. In WhatsApp, open **Settings → Linked Devices → Link a Device** (on Android: **⋮ → Linked devices → Link a device**) and scan the QR code. If the window is too small for the code, maximize it. If scanning doesn't work, choose the pairing code option and type the 8-character code on your phone.
4. Keep WhatsApp open on your phone, with the screen unlocked, until it finishes. If the phone locks or WhatsApp is closed, the progress bar shows "Paused: waiting for your phone" and carries on by itself when you open WhatsApp again.

When it's done, a summary shows the totals and the ZIP is saved on your **Desktop**, then shown in Finder (Mac) or File Explorer (Windows). It's named after your number and the month, for example `15551234567-11-26.zip` (a quick test adds `-test`). On a Mac, the first time, macOS may ask whether Terminal can access your Desktop: click Allow. If the Desktop can't be used, the ZIP is saved in the **WhatsApp Exporter** folder in your home folder instead, and the summary says so. The exporter then unlinks itself from your phone, deletes its working files so only the ZIP is left, and closes the window.

If it stops partway (the computer sleeps, Wi-Fi drops, you press Ctrl+C), paste the same line again and choose to continue. It remembers whether it was a quick test or a full export and picks up where it left off.

## What's in the ZIP

- One folder per chat: the contact's name (or phone number if they aren't saved) for one-to-one chats, and `GP-<group name>` for groups.
- Each folder holds `_chat.txt` and that chat's media. Media files are named only by the attachment ID used in the chat text, for example `00000001.jpg`, which `_chat.txt` refers to as `<attached: 00000001.jpg>`. For documents, the original file name appears in the chat text before the attachment.
- `index.csv`: every chat with its number, profile name, message counts and date range.
- `report.txt`: totals, plus a list of any media that couldn't be exported and why.

Individual, group and archived chats are included. Status updates, channels and broadcast lists are not.

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

This produces `dist/WhatsApp Exporter` (a universal Mac app for Apple Silicon and Intel) and `dist/WhatsApp Exporter-windows-amd64.exe` and `-arm64.exe`. Run the tests with `go test ./...`. By default the ZIP goes to the Desktop (Windows asks the system for it, so a Desktop moved into OneDrive is found) and the working data goes next to the app. The `-dir` flag puts both in the given folder instead.

The Mac and Windows versions share all their code. The few differences live in `platform_darwin.go` and `platform_windows.go`: showing the ZIP in Finder or File Explorer, making the window big enough for the QR code, and plainer symbols in the older Windows console. The Mac app closes its own Terminal window at the end; on Windows, `install.ps1` closes PowerShell after a successful run.

While running, the exporter keeps its login, a message database and downloaded media in a `WhatsApp Exporter Data` folder next to the app, so an interrupted export can resume. A log file for troubleshooting is in there too (`exporter.log`). Set `WHATSAPP_EXPORTER_DEBUG=1` for more detail. The whole folder is deleted once an export succeeds.

### Releasing

```bash
./release.sh
```

Downloads are served from an unlisted GitHub Gist (`74929d87bb6c220c23bea4d9d1b37377`), so they can be fetched by anyone with the link but don't show up in search. This source repository is private.

`release.sh` needs a clean working tree and the GitHub CLI signed in as the gist's owner. It regenerates `THIRD-PARTY-NOTICES.txt`, runs the tests, builds the Mac and Windows apps, and replaces the gist's contents with the compressed apps, both install scripts, this README, the licence files and `whatsapp-exporter-source.tar.gz` (the source of the exact commit being released). It then pushes `main` to this repository. The install lines always fetch whatever is in the gist; GitHub may take a few minutes to serve the new files.

### Licence

WhatsApp Exporter is free software under the GNU General Public License v3; see [LICENSE](LICENSE). It includes third-party libraries under their own licences, listed in [THIRD-PARTY-NOTICES.txt](THIRD-PARTY-NOTICES.txt). The complete source for every release is published next to the downloads as `whatsapp-exporter-source.tar.gz`.

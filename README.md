# WhatsApp Exporter

Saves every chat, photo and file from your WhatsApp or WhatsApp Business account into one ZIP on your Mac, laid out like WhatsApp's own "Export chat". It runs entirely on your Mac: nothing is uploaded anywhere.

## Run it

1. Open **Terminal**: press ⌘ Space, type `Terminal`, then press Enter.
2. Paste this line and press Enter:

   ```bash
   /bin/bash -c "$(curl -fsSL https://github.com/GirumDoctours/whatsapp-exporter/releases/latest/download/install.sh)"
   ```

3. Use the arrow keys and Enter (or Space) to choose:
   - **Run a quick test**: your 10 latest chats, in about 3 minutes.
   - **Export everything**: every chat, photo and file. About 25 minutes for a busy account, longer for very large ones.
4. Link your phone. In WhatsApp, open **Settings → Linked Devices → Link a Device** (on Android: **⋮ → Linked devices → Link a device**) and scan the QR code. If scanning doesn't work, choose the pairing code option and type the 8-character code on your phone.
5. Keep WhatsApp open on your phone, with the screen unlocked, until it finishes. If the phone locks or WhatsApp is closed, the progress bar shows "Paused: waiting for your phone" and carries on by itself when you open WhatsApp again.

When it's done, a summary shows the totals and Finder opens on your ZIP. It's in the **WhatsApp Exporter** folder in your home folder, named after your number and the month, for example `15551234567-11-26.zip` (a quick test adds `-test`). The exporter then unlinks itself from your phone, deletes its working files so only the ZIP is left, and closes the window.

If it stops partway (the Mac sleeps, Wi-Fi drops, you press Ctrl+C), paste the same line again and choose to continue. It remembers whether it was a quick test or a full export and picks up where it left off.

## What's in the ZIP

- One folder per chat: the contact's name (or phone number if they aren't saved) for one-to-one chats, and `GP-<group name>` for groups.
- Each folder holds `_chat.txt` and that chat's media. Media files are named only by the attachment ID used in the chat text, for example `00000001.jpg`, which `_chat.txt` refers to as `<attached: 00000001.jpg>`. For documents, the original file name appears in the chat text before the attachment.
- `index.csv`: every chat with its number, profile name, message counts and date range.
- `report.txt`: totals, plus a list of any media that couldn't be exported and why.

Individual, group and archived chats are included. Status updates, channels and broadcast lists are not.

## Good to know

- Needs macOS 13 or newer, on Apple Silicon or Intel.
- History comes from your phone. Anything it no longer has (deleted messages, media it has cleared, history lost in a reinstall without a backup) can't be exported.
- Media older than a few weeks has to be re-sent by the phone, so it's slower. Files the phone no longer has are listed in `report.txt` as "no longer on the phone".
- This is an unofficial tool, not made by or affiliated with WhatsApp or Meta. It links to your account as a linked device, the same way WhatsApp Web does, using the [whatsmeow](https://github.com/tulir/whatsmeow) library. Unofficial clients are against WhatsApp's terms of service. Read-only use like this is low risk, but not zero.

## For developers

### Building

Requires Go 1.27+ and the Xcode command line tools.

```bash
./build.sh
```

This produces a universal (Apple Silicon and Intel) binary at `dist/WhatsApp Exporter`. Run the tests with `go test ./...`. The `-dir` flag sets where the ZIP and working data go; by default it's the folder the app is in.

While running, the exporter keeps its login, a message database and downloaded media in a `WhatsApp Exporter Data` folder next to the app, so an interrupted export can resume. A log file for troubleshooting is in there too (`exporter.log`). Set `WHATSAPP_EXPORTER_DEBUG=1` for more detail. The whole folder is deleted once an export succeeds.

### Releasing

```bash
./release.sh
```

This needs a clean, pushed `main` branch and the GitHub CLI signed in to an account that can publish to this repository. It regenerates `THIRD-PARTY-NOTICES.txt`, builds the app, and publishes a GitHub release tagged with the date and time, pointing at the current commit. The release holds `WhatsApp-Exporter-mac.gz` and `install.sh`. The install line above always downloads the latest release.

### Licence

WhatsApp Exporter is free software under the GNU General Public License v3; see [LICENSE](LICENSE). It includes third-party libraries under their own licences, listed in [THIRD-PARTY-NOTICES.txt](THIRD-PARTY-NOTICES.txt).

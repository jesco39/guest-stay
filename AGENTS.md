# guest-stay — agent notes

Quick reference for working on this project with an AI assistant. For architecture and file-by-file descriptions, see the top-level `CLAUDE.md`.

## Life calendar conventions

Availability is derived from all-day events on the Google calendar named by
`GOOGLE_LIFE_CALENDAR_ID`. Their titles are load-bearing — the app reads meaning out of
them in `classifyEvent` (`calendar.go`), and both the blocked-dates read and the
host-availability read go through that one function so they cannot disagree.

| Event title | Meaning |
|-------------|---------|
| Starts `Guest Stay:` or `Cat Sitting:` | An approved booking. **The only thing that blocks dates.** Written by the app on approval; do not create by hand |
| Names Jesse **and** Allison | Both hosts away — available for cat sitting |
| Names only Jesse, or only Allison | That host away, the other home — available as a regular guest stay |
| Names neither host | Both hosts away — available for cat sitting |

Two consequences worth knowing before adding events to that calendar:

- **Any all-day event that is not a booking opens the dates.** An event naming neither
  host reads as travel, so a hold that is not travel — "contractors in", a visitor
  staying over — would make those dates bookable rather than blocking them. Guest stays
  belong in the app, which writes its own blocking event on approval.
- **Host matching is substring-based** on "jesse" and "allison", case-insensitive. A
  mistitled event silently misclassifies the day.

Timed (non-all-day) events are ignored entirely.

## Deploy

```bash
./deploy.sh https://guest-stay.jesco39.com
```

The script accepts a URL, hostname, or IP and figures out the rest:

- **URL / hostname:** resolved to an IP via `dig +short`. We always connect by IP because `~/.ssh/known_hosts` has the VM's host key pinned to its IP, not its DNS name — passing a hostname to plain `ssh` triggers host-key verification failure.
- **SSH user:** defaults to `jesco` (matches the project-level SSH key in the GCP project metadata). Override via the second positional arg if needed.
- **SSH key:** the script auto-runs `ssh-add ~/.ssh/google_compute_engine` before deploying. That key is gcloud-managed and isn't a default SSH identity, so plain `ssh`/`scp` won't try it unless it's in the agent.

### Finding the VM

If the DNS record is ever missing or stale:

```bash
gcloud config set project jesco39-com
gcloud compute instances list
```

The VM is named `dunnage` in zone `us-central1-a`, in GCP project `jesco39-com`. It hosts multiple apps (guest-stay, cask). Its external IP is what you pass to `deploy.sh`.

### What deploy.sh does

1. Cross-compiles the Go binary for `linux/amd64`.
2. `scp`s the binary, `templates/`, `static/`, `deploy/`, and (if present) `credentials.json` to `/tmp` on the VM.
3. `sudo mv`s them into `/opt/guest-stay`, fixes ownership to `guest-stay:guest-stay`.
4. `sudo systemctl restart guest-stay` and prints the service status.

The systemd unit is at `/etc/systemd/system/guest-stay.service` on the VM.

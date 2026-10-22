# Keygate on an OVH VPS (Debian 13) - ALC licensing server

Target: `api.alc-control.com` -> VPS `57.131.165.209` (Frankfurt). Blitz stays the test environment and is never touched by these scripts.

## Architecture

```
Internet --443/80--> Caddy (automatic HTTPS, reverse proxy, no access log)
                       --127.0.0.1:9000--> keygate  (systemd, user "keygate", hardened, read-only release)
                                              --127.0.0.1:5432--> PostgreSQL (local only, scram password)
                                              --587 STARTTLS-->  OVH mailbox (smtp.mail.ovh.net)
                                              --443-->           Stripe API (test key only, optional)
```

* Application: Go binary + `web/dist` + `db/migrations` + `docs`, installed under `/opt/keygate/releases/<id>/`, `/opt/keygate/current` is a symlink switched atomically. Migrations run at start-up (forward only).
* Persistence: **only PostgreSQL** (database `keygate`). The server writes nothing else to disk (S3 storage disabled).
* Secrets: `/etc/keygate/keygate.env` (`root:root 0600`, read by systemd, not readable by the service account), `/etc/keygate/backup.pass` (`0400`). Nothing secret is in Git.
* Ports: 22 (existing), 80 and 443 (opened by `keygate-enable-https` only after the DNS check). 9000 and 5432 are not exposed (UFW default deny + PostgreSQL on localhost). Port 5355 (LLMNR) is disabled by `bootstrap.sh`.
* Hardening: dedicated account without shell, systemd sandboxing, unattended security upgrades, fail2ban (sshd), health-check timer (restart after 3 failed checks), daily encrypted backups.
* Client IP: Keygate trusts `X-Forwarded-For` only from the CIDRs in `TRUSTED_PROXIES` (`127.0.0.1/32,::1/128` here, because Caddy is local). Without it every visitor looks like 127.0.0.1 and rate limits / brute-force guards hit everybody at once.

## First installation (from your Mac, in the repository)

1. Authorise the deployment key once (your VPS password is typed in YOUR terminal, never shared):
   `ssh-copy-id -i ~/.ssh/alc_ovh_ed25519.pub debian@57.131.165.209`
2. `deploy/ovh/deploy.sh --bootstrap`  (builds the release, prepares the server, installs the release, checks health)
3. On the VPS: `sudo keygate-set-secret SMTP_PASSWORD`  (hidden prompt), then `sudo systemctl restart keygate`.
4. DNS (at OVH, **you** do it): `api.alc-control.com.  A  57.131.165.209  (TTL 300)`. Add `AAAA` only if the VPS has a working public IPv6 address. Nothing else changes: the website, the MX and SPF records stay as they are.
5. On the VPS: `sudo keygate-enable-https` (checks the DNS, opens 80/443, starts Caddy, verifies the real certificate).
6. Open `https://api.alc-control.com`, sign in with the admin code (mailed to `contact@alc-control.com`; until the SMTP works it is in `sudo journalctl -u keygate`), then create the product **Autotune Live Control** and the plan **ALC Perpetuelle** (perpetual, 1 activation, token TTL 7 days, the Stripe price id later).
7. Put the new product id in `/etc/keygate/keygate.env` as `ALC_KEY_PRODUCT_IDS=<id>` and `sudo systemctl restart keygate` (new keys then use the `ALC-` format).

## Mail (OVH mailbox)

`SMTP_HOST=smtp.mail.ovh.net`, `587` STARTTLS, user `contact@alc-control.com`. Diagnose with `sudo keygate-smtp-check` (DNS, TCP, TLS, AUTH offered; no credentials). Real test: `sudo keygate-smtp-test-send you@example.com`, then confirm the message arrived. If 587 is blocked or refused, the diagnosis says why; then try `465` (implicit TLS, Keygate chooses it by port) or the classic MX Plan host `ssl0.ovh.net`.
Nothing is "operational" until a message has actually been received.

## License signing key (Ed25519) - strategy

* `bootstrap.sh` generates a **new** `LICENSE_SIGNING_KEY` for this server. The Blitz key is not copied (it would put a test-environment secret on production, and Blitz must keep working unchanged).
* ALC pins the **public** key. Production builds embed the VPS public key (`ALC_LICENSE_PUBLIC_KEY_HEX`, `ALC_LICENSE_API_URL`, `ALC_LICENSE_PRODUCT_ID` compile-time macros, or `server.json` for tests). Proofs from Blitz do not verify against the VPS key and vice versa, so the two environments cannot be mixed up.
* Read the public key from the running server: `curl https://api.alc-control.com/api/v1/license/pubkey`.
* Changing the key later invalidates every issued proof (clients cannot renew until they know the new public key). Treat it as permanent; it is inside the encrypted backup. A multi-key transition in ALC is not implemented.
* There is no licensing in the iOS / Android builds today, so no mobile compatibility issue.

## Stripe (test mode) and the second server

Stripe is OFF until you enable it: `sudo keygate-set-secret STRIPE_SECRET_KEY` (an `sk_test_...` key) and restart. This server then creates **its own** webhook endpoint (`https://api.alc-control.com/api/v1/webhook/stripe`) with its own secret; Blitz's endpoint is not modified. Both endpoints receive every test event of the account, so a test purchase would also create a license on Blitz: disable or delete Blitz's endpoint in the Stripe dashboard (test mode) when you switch, or keep it in mind. Live mode: only with a deliberate decision, never during these tests.

## Backups and restore

* Daily at ~03:15 (systemd timer): `pg_dump` + the environment file, tar, AES-256 (`/etc/keygate/backup.pass`), verified (decrypt + archive + `pg_restore --list`), in `/var/backups/keygate` (`0700`). Retention: 14 newest + Sunday backups for 8 weeks. Status: `cat /var/lib/keygate-backup/last_success`, `journalctl -t keygate-backup`.
* After each successful backup, the script stages only the retained encrypted archives and SHA-256 sidecars in `/home/debian/keygate-export` (`0700`, `debian:debian`); the decryption passphrase is never copied there.
* The Mac sync script is `deploy/ovh/mac-sync-backups.sh`. Its dedicated key (`~/.ssh/keygate_backup_sync_ed25519`) is forced to `/opt/keygate/deploy/keygate-backup-export.sh` through a restricted `authorized_keys` entry. It can export only encrypted archive files and checksums, not open a shell or forward ports. The LaunchAgent `com.alc.keygate-backup-sync` runs daily at 07:30 local time and syncs verified files to iCloud Drive `Keygate-Backups`. The local archive set mirrors server retention only after every incoming checksum validates.
* **Still required: copy `/etc/keygate/backup.pass` into a trusted password manager or another secure recovery store outside the VPS.** Never paste it into chat, a ticket, or a log. The current encrypted archive is not recoverable without it. OVH's own VPS backup option is a complement, not a replacement.
* Test a backup (touches nothing live): `sudo keygate-restore /var/backups/keygate/<file> --test`.
* Restore for real: `sudo keygate-restore <file> --apply --yes [--with-env]` (the current database is kept under a new name, never dropped).

## Updating

From the Mac: `deploy/ovh/deploy.sh` (build, upload, backup, atomic switch, health check, **automatic rollback** if the new release is unhealthy). The deployed version is visible at `/api/v1/version` (commit + branch).
Manual rollback: `sudo ln -sfn /opt/keygate/releases/<previous> /opt/keygate/current && sudo systemctl restart keygate` (if a migration ran, restore the pre-update backup instead).

## Checks

`systemctl status keygate caddy postgresql`, `curl -fsS http://127.0.0.1:9000/health`, `sudo ss -ltnup`, `sudo ufw status`, `sudo journalctl -u keygate -n 50` (no secret appears in the logs; the OTP code is printed only while SMTP is not configured).

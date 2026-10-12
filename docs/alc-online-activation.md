# ALC online activation (Keygate <-> Autotune Live Control)

Keygate already provides the whole server side; ALC (desktop: macOS / Windows) now uses it.

## Endpoints used by ALC (all under `/api/v1`)

| Call | Purpose |
|---|---|
| `POST /license/activate` | First activation. Body: `license_key`, `identifier` (ALC device id, a hash), `identifier_type: "device"`, `label` ("ALC <OS name>"). Sends an `Idempotency-Key` header reused for the retries of ONE attempt. Atomic: the row of the license is locked, a second device gets `409 ACTIVATION_LIMIT`, the same device gets `already_activated` without using a slot. Returns `data.token`, the signed proof. |
| `POST /license/verify` | Renewal of the proof for an already activated device. A revoked / suspended / expired license or an unknown device gets `404 LICENSE_NOT_FOUND` (no new proof). |
| `GET /license/pubkey` | Ed25519 public key (hex) used to configure ALC. Fetch it once over a trusted channel; ALC never trusts a key received at activation time. |
| `POST /license/deactivate` | Existing; not used by ALC. Releases the device slot given key + identifier. |
| Admin: license detail > Activations > trash icon | `DELETE /admin/activations/:id`, audited (`activation deleted`): the authorised way to free the slot for a transfer. |

## Proof (signed token)

`base64url(payload).base64url(ed25519 signature)`; the signature covers the base64url payload. Payload fields: `lid` license, `pid` product, `pln` plan,
`sts` status, `did` device id, `iat`/`exp` (offline window = the plan's token TTL, 7 days here), `vun` license end (absent = perpetual), `grc`, `nce`, `fpr`.
The private key never leaves the server (`LICENSE_SIGNING_KEY`, 32-byte seed in hex, already required by Keygate). ALC embeds nothing secret.

## Offline policy implemented in ALC

* License duration (perpetual) is distinct from the **offline authorisation** (`exp`, 7 days).
* A valid proof allows full offline use. From half of its life it is renewed in the background when the server answers.
* Server down: a still-valid proof keeps working. After `exp`: ALC asks for a connection (activation screen, automatic renewal attempt).
* Revoked / suspended license: no new proof; ALC stops when the last proof expires (at most one offline window, 7 days).
* Clock moved back (more than 24 h before the last observed date or before the issue date): a connection is required.
* Proof for another device / product, forged or signed by another key: rejected, local state left untouched.

## Configuration

Keygate: nothing new. Required as before: `LICENSE_SIGNING_KEY` (keep it stable: rotating it invalidates every proof and ALC's configured public key),
`ALC_KEY_PRODUCT_IDS=<ALC product id>` for the key format, the plan `max_activations = 1` and the plan token TTL (7 days).

ALC (no hard-coded endpoint): `server.json` in `~/Library/ALC_License/` or `~/Library/Application Support/ALC_License/` on macOS (JUCE keeps its
"application data" in `~/Library`, next to `lk.dat`; both are read, the first wins), `%APPDATA%\ALC_License\server.json` on Windows.
An existing but unusable file (invalid JSON, non-HTTPS URL, bad public key) is reported to the user and activation is refused: there is no silent
fallback to local activation. Diagnostics (no key, no secret, no full proof) are written to the console as `[ALC licence] ...`.

```json
{ "api_url": "https://<keygate host>/api/v1", "public_key": "<64 hex from GET /license/pubkey>", "product_id": "<Keygate product id>" }
```

The same values can come from the environment (`ALC_LICENSE_API_URL`, `ALC_LICENSE_PUBLIC_KEY_HEX`, `ALC_LICENSE_PRODUCT_ID`) or compile-time macros of the same names.
Without a complete configuration (HTTPS URL, or http://localhost for tests, + 64-hex key) ALC keeps its historical local activation.
With it, a locally activated license without proof is asked to re-activate online once.

## Blitz test instance

A free Blitz app goes to sleep and a program cannot wake it ("This app is asleep"): open `https://keygate-test.keygate-alc.blitz.cloud` in a browser first,
then ALC (or `curl`) can reach it. The production server (OVH) will not sleep.

## Security limits

* The ALC key format keeps its public checksum and ~40 random bits: the server, not the key, is the authority (the proof).
* The proof binds license + product + device id; the device id is the existing ALC hardware hash (pseudonymous, no e-mail), also readable in the admin.
* There is no ALC-side "deactivate" button: transfers go through an admin release (audited).
* If the user clock is wrong by more than the tolerance, a connection is required; a determined attacker who can also block the network can still replay
  an old proof until its `exp` (<= 7 days).

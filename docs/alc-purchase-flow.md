# ALC purchase flow (Stripe test mode) - state and manual steps

```
ALC "ACHETER UNE LICENCE" -> Stripe Payment Link / Checkout (test mode)
  -> checkout.session.completed webhook  (signature + API version + livemode + event claim)
  -> ONE license (session claim + unique index), product/plan resolved from plan_id metadata, the subscription or the line-item price
  -> key in ALC format for the ALC product (ALC_KEY_PRODUCT_IDS) -> e-mail queued (persistent queue, retried)
  -> customer pastes the key in ALC -> online activation (see alc-online-activation.md) -> admin shows 1 / 1
```

## What the code guarantees (all pre-existing Keygate behaviour, now covered for ALC by `TestALCPurchaseToActivation`)

* Webhook: Stripe signature checked (previous secret accepted during an endpoint replacement), unsupported API versions refused, a test-mode event is refused by a live server and vice versa.
* Idempotency: a replayed event is ignored (event claim); two different events for the same session still give one license (session claim + unique index on the checkout session).
  A second paid session gives a second license (one payment, one license).
* Unpaid / delayed-payment sessions create nothing until `async_payment_succeeded`; unresolved plan or missing e-mail are logged and retried by the periodic sync.
* Failure after payment: if the license cannot be written (or its key cannot be generated), the session claim is released so the webhook retry, the success page
  (`/api/v1/checkout/verify`) or the periodic sync can fulfil it later: no double license, no double activation.
* E-mail: queued in the database right after the license exists (never lost when SMTP is down): transient failures keep the body for retry, permanent bounces are terminal.
  An admin can resend it from the license page. The e-mail carries the key unchanged; logs never contain a full key.
* The key from the e-mail activates online on one device; a second device is refused (`ACTIVATION_LIMIT`).

## Manual configuration still needed (test environment)

1. Blitz environment: `STRIPE_SECRET_KEY=sk_test_...` (test mode only; the live flag is derived from the key prefix) and a correct public `BASE_URL`.
   Keygate then creates / refreshes its own webhook endpoint (`/api/v1/webhook/stripe`) at startup; check the Stripe dashboard (test mode) for it and, if you create it by hand,
   set `STRIPE_WEBHOOK_SECRET` and subscribe at least `checkout.session.completed`, `checkout.session.async_payment_succeeded`, `checkout.session.async_payment_failed`, `charge.refunded`.
2. Keygate admin > Plans > "ALC Perpetuelle": set the **Stripe price ID** of the one-time 29.99 EUR price that the Payment Link sells (this is how a Payment Link, which has no metadata, is mapped to the plan).
3. Stripe (test mode) Payment Link: after-payment redirect to the Keygate success page if you want the verify fallback (`.../checkout/success?session_id={CHECKOUT_SESSION_ID}`); put its `https://buy.stripe.com/test_...` URL in
   ALC's `server.json` as `"purchase_url"` (otherwise ALC opens its historical test link).
   Alternative without a Payment Link: Keygate's own unified checkout `https://<keygate host>/pay/<plan checkout id>` (id shown on the plan page) creates the Checkout Session
   with `plan_id` metadata and the success redirect for you; use that URL as `purchase_url` and no Stripe price mapping is needed.
4. E-mail: Blitz blocks SMTP ports and the only dashboard provider is Cloudflare Email (HTTPS, needs a Cloudflare account and a verified sender). Until a provider works, the mail stays
   queued: read the key in the admin (license page > reveal, audited) or resend later. OVH SMTP (`ssl0.ovh.net`) is for the future VPS; no DNS / mailbox change was made.
5. Suggested text for the "License created" template (Settings > Email Templates), e.g.: "In Autotune Live Control, open the license screen, paste this key and click Activate (internet connection required once)."

## Short end-to-end test (Stripe test mode)

1. Open the Keygate test URL once in a browser (free Blitz apps sleep).
2. In ALC: "ACHETER UNE LICENCE" -> pay with the test card `4242 4242 4242 4242`, any future date / CVC, an address you can read.
3. Keygate admin > Licenses: one new `ALC-` license for Autotune Live Control / ALC Perpetuelle, `0 / 1`. Audit log shows `created` by `stripe`/webhook.
4. Read the key (license page, reveal) or from the e-mail if a provider is configured; paste it in ALC and activate: `1 / 1`.
5. Replaying the webhook from the Stripe dashboard (test mode) must not create a second license.

## Not verified here

No real Stripe call, no real e-mail delivery and no graphical purchase were possible in this environment; the webhook was exercised with really signed events against a stubbed Stripe API.

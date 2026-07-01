# DDoS and resource-exhaustion posture

No application alone is "DDoS proof": volumetric floods are absorbed at the edge,
not the origin. RedPaste is built to survive application-layer abuse and to sit
behind a DDoS-capable edge. Both layers matter.

## What RedPaste does itself

- **Load shedding:** a global in-flight request cap (`REDPASTE_MAX_INFLIGHT`,
  default 512). Past it, requests get `503` immediately instead of piling up and
  exhausting memory or goroutines. WebSocket connections bypass this counter and
  are bounded separately.
- **Per-IP rate limits** on every abusable action (create, redeem, metadata,
  live-create, live-join), trusted-proxy aware so limits key off the real client
  IP, not the proxy.
- **Slow-loris guards:** `ReadHeaderTimeout` 8s, bounded `ReadTimeout`,
  `WriteTimeout`, `IdleTimeout`, and a 64 KiB max header size.
- **Body caps:** oversized requests are rejected with `413` before large buffers
  are allocated.
- **WebSocket limits:** first-message auth, per-message size cap, per-connection
  message-rate cap, bounded outbound queue with slow-consumer disconnect, room
  member cap, and a per-IP connection cap. No unbounded goroutines are spawned
  from client input.
- **Storage caps** on live rooms (snapshot/update size and total bytes) so a room
  can't be used as unbounded storage.
- **Bounded pools:** database connection pool limits; single-writer SQLite.

## What the edge must do (required for real protection)

Front RedPaste with a DDoS-capable edge (this deployment uses Cloudflare):

- Keep the origin IP hidden; allow inbound only from the edge (origin firewall).
- Turn on the provider's L3/L4 volumetric protection (always-on with Cloudflare).
- Enable Bot Fight Mode / managed challenges, but **exclude the API paths that
  legit automation and the live socket use** (`/api/v1/live/*`) so challenges
  don't break WebSockets or JSON POSTs.
- Add rate-limiting rules at the edge for `/api/v1/pastes` and
  `/api/v1/live` creation.
- Use "Under Attack" mode as a break-glass control during an active flood.
- Terminate TLS at the edge/reverse proxy; nginx enforces a request-body cap and
  forwards only to loopback.

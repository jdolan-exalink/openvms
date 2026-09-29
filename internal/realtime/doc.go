// Package realtime serves the multiplexed push feed at GET /ws.
//
// Flow: publishers (the worker) put typed messages on NATS JetStream; every API instance
// runs one ephemeral ordered consumer per stream (Feed) that hands each message to the Hub;
// the Hub decodes it through the Route registry into an Envelope plus the Scope needed to
// authorize it, and offers it to every connected Subscription.
//
// Wire format, one JSON text frame per message:
//
//	{"type": "event.created", "tenant_id": "<uuid>", "data": {...}}
//
// Message types today: "event.created" (a new event entered the index; requires events.view
// on the camera) and "server.status" (a server's health changed; requires servers.view on the
// server). Payloads are deliberately thin identifiers and states: clients refetch through the
// REST API, which stays the source of truth and applies its own redaction (plates, sub-labels).
//
// Filtering, applied per connection and never widening what the list endpoints return:
//
//  1. Tenant: a tenant user only ever receives messages of its own tenant. This check does
//     not depend on the Authorizer.
//  2. Permission: the Authorizer must confirm the actor holds the route's permission on the
//     message's camera or server, using the same authorized-ID queries as the list endpoints.
//     Any error denies the message (fail closed).
//
// Robustness: each connection has a bounded buffer and is closed when it overflows (the UI
// then falls back to polling and reconnects), pings keep idle connections honest, the number
// of connections per user is capped, and Hub.Close ends every connection on server shutdown.
// The JetStream consumer is watched for any stop (terminal error or a stopped consume
// context) and recreated with exponential backoff (2s doubling to 30s, reset after a healthy
// minute); every restart is logged.
//
// Extending the feed (alarms, notifications): add a Route with a new Type constant, a subject
// on a stream in natsx.Streams, and a Decode that returns the Scope to authorize against.
// Nothing else changes.
//
// Authentication is not handled here: the router's Authenticate middleware protects /ws with
// the same session cookie or bearer token as the API and media gateway, and the origin policy
// is httpx.OriginAllowed, shared with the media gateway. The credential is then re-validated
// every 30s through Handler.Session (built from the same lookup, read-only, so it never
// slides the idle timeout). Close codes a client must handle: 1008 "session ended" (logout,
// revocation, expiry, disabled user: re-authenticate, do not blindly reconnect), 1013
// "session check unavailable" (two consecutive undecidable checks: reconnect with backoff),
// 1008 "slow consumer" and 1001 "server shutting down" (reconnect).
package realtime

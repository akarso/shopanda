package cache

// EventInvalidated fires when a cache.Cache backend's DeleteByTag or
// DeleteByPrefix removes entries (PR-1040), so an in-process L1 cache
// tier sitting in front of that backend can evict its own copies
// immediately instead of waiting out its own TTL.
//
// This only ever reaches other subscribers within the SAME OS process:
// internal/platform/event.Bus is a plain in-process pub/sub with no
// cross-replica delivery mechanism (no Redis pub/sub, no network I/O),
// and every server process (serve, worker, each scaled-out replica of
// either) constructs its own independent *event.Bus. Publishing this
// event does NOT propagate to other replicas — a short L1 TTL is the
// actual bound on cross-replica staleness; this event only shortens
// same-process staleness to "however long dispatch takes" (typically
// sub-millisecond). See PR-1040's own "Design decisions" for the full
// reasoning.
const EventInvalidated = "cache.invalidated"

// InvalidatedData is the payload for EventInvalidated. Exactly one field
// is set, matching whichever call (DeleteByTag or DeleteByPrefix)
// triggered it.
type InvalidatedData struct {
	Tag    string `json:"tag,omitempty"`
	Prefix string `json:"prefix,omitempty"`
}

# Consistency

Events are organized into ordered streams.  Loose ordering is used by default, allowing all events to be written
optimistically.  Applications may request stricter ordering and conflict detection when desired.  Ordering is controlled
via event coordinates within a stream.

## Draft Notice

This document is currently in draft state and is subject to change. It provides an overview of the optimistic
consistency model for multiple instances of `pgcqrs`, but final details and implementation specifics may vary.

## Event Coordinates A single event stream is an ordered log of events.  Events are uniquely identified via a pair of
`nexus` and `id` for location within the stream.  `id` will monotonically increase for any given `nexus`.  `nexus`
identifies a unit of control for the stream.  In most cases a `nexus` will be an instance of `pgcqrs`.

## Optimistic
Concurrency

In optimistic concurrency, events are considered non-conflicting by default. This means that multiple instances can
write events independently without immediate conflict detection.

In the suggested edit, a client may consider two events conflicting if they share one of a set of predefined types. This
approach assumes that conflicts are rare and can be resolved later if they occur.

This model is particularly useful in distributed systems where performance and availability are prioritized over strict
consistency.

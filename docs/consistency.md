# Consistency

Events are organized into ordered streams.  Loose ordering is used by default allowing all events to be written
optimistically.  Applications may request stricter ordering and conflict detection when desired.  Ordering is
controlled via event coordinates within a stream.

## Event Coordinates
A single event stream is an ordered log of events.  Events are uniquely identified via a pair of `nexus` and `id` for
location within the stream.  `id` will monotonically increase for any given `nexus`.  `nexus` identifies a unit of
control for the stream.  In most cases a `nexus` will be an instance of `pgcqrs`.

A `nexus` is queryable to understand consistency with others.  For example, if we had two instances of `pgcqrs`.
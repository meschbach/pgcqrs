# Next Protocol Changes

These are changes which I would like to make in the next revision of the wire protocol.

## Envelope Dates
* Client should only see this as a date, or the ability to convert from a date.
  * Rationale: In many cases a client does not actually care about the exact date.  Sequencing itself is handled by the
stream.  So we should avoid the cost of conversion unless we know it is wanted.
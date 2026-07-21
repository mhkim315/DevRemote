# Input-B evidence

Input-B adds acknowledgement of terminal acceptance without changing Input-A's
server-authorized permission gate. A binary frame is acknowledged only after
the exact `TerminalTransport` captured during WebSocket establishment writes
every input byte successfully.

The server hello frame carries the captured generation. Each successful input
returns an `input_ack` control frame containing that same generation and a
per-connection monotonic sequence. The reader never resolves a transport by
session ID after the connection is established, so replacement cannot cause an
old connection to receive an ACK for a newer generation. Short writes, write
errors, and retired transports produce no acceptance ACK.

`TestInputB_HandleWSAcknowledgesExactGenerationAfterWrite` drives ticket-auth
`HandleWS`, sends a binary frame, verifies the writer received it, and asserts
the returned `input_ack` has generation 7 and sequence 1.
`TestInputB_HandleWSWriteFailureDoesNotAcknowledge` drives the same path with a
failing writer and proves it emits no `input_ack`.

The terminal page reports each socket enqueue to the native client as
`input_pending` with the generation/sequence. `FeedScreen` treats this only as
"Sent to socket". It shows "Delivered to terminal" only after the matching
server ACK, or "Not delivered" if no ACK arrives within three seconds; it does
not automatically replay the input.

Validation:

```text
go test -race ./... -count=1 -timeout 90s
go vet ./...
go build ./...
cd mobile && npm test -- --runInBand && npm run typecheck
```

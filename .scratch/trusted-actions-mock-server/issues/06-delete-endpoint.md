# 06 — Delete endpoint conformance and observable teardown

**What to build:** A developer deletes a trusted action and receives the contract's JSON response, so a
generated client can decode the deletion result. The current implementation returns `text/plain`, which
causes a generated client to decode nothing.

Deleting an unknown trusted action returns the contract's error envelope, so not-found handling is
exercised correctly.

After deletion the proxy stops working, so teardown is observable rather than assumed.

**Blocked by:** 02, 03

**Status:** done

## How to demo

Create an action, keep its `proxyUri`, and confirm a proxy call works. Then:

```bash
curl -s -D- -X DELETE localhost:8080/backplane/trustedactions/$C1/demo--$UUID
```

Read the headers, not just the body: `Content-Type` must be `application/json` and the body must parse
with `jq`. Today it is `text/plain` and a generated client silently decodes nothing, so a demo that
only eyeballs the text would pass before this work.

Then re-run the proxy call that worked a moment ago and confirm it now fails, and DELETE the same
instance a second time for the not-found envelope.

## Acceptance criteria

- [x] A successful delete removes the instance and responds with the contract's JSON string body.
- [x] The response content type is JSON, and the body decodes with a generated client.
- [x] Deleting an unknown trusted action responds with the contract's error object.
- [x] A proxy call that succeeded before the delete fails after it.

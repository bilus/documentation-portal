# Ledger, plan: docs/plans/2026-09-29-do-not-publish.md

- 2026-09-29 plan drafted for bilus/documentation-portal#19 on bilus/development, from b7413f3; questions Q1 to Q8, each with an assumption. The endpoints file of conrad-api-docs carries 1 of the 11 markers, on a schema property, for main; the other 10 are in delivery_common.oas2.yml, which the user set aside.
- 2026-09-29 vocabulary: raw spec now leaves out the unpublished parts and the marker keys; added unpublished part and marker key.
- 2026-09-29 design: no diagram change; the raw spec's handler belongs to box 3.3, as decided for #13 (Q5).
- 2026-09-29 holes added: 1 portal.publishedSpec, called by portal.site.rawSpec. Acceptance tests written and skipped: 1 TestPublishedSpec, 1 TestRawSpecLeavesOutUnpublishedParts.
- 2026-09-29 reviews: the design and vocabulary reviews run once, before the pull request (the user's rule).
- 2026-09-29 plan approved by the user. Signatures, top diagram and vocabulary frozen; score card cached.
- 2026-09-29 stage 1 started from 4aec322.
- 2026-09-29 filled 1 portal.publishedSpec: a spec whose text lacks x-doNotPublish passes through untouched; otherwise yaml.v3 parses it as a yaml.Node, one walk drops the marked mappings, list elements and siblings for main and every marker key, and the encoder writes it with two-space indents. Unskipped TestPublishedSpec and TestRawSpecLeavesOutUnpublishedParts, which fail against the mock body. Checked on the endpoints file of conrad-api-docs: the served spec equals the source minus components/schemas/baselineBroadcastData/properties/romanizedName, parsed and compared as data; 385 KB against 400 KB, 22 to 25 ms a request; Elements renders it without the property after a reload.
- 2026-09-29 stage 1 complete: gate green (make build lint test test-e2e, and go test -count=1 with and without the e2e tag), TestPublishedSpec and TestRawSpecLeavesOutUnpublishedParts pass unskipped, the census is empty, score unchanged, about 90 net lines, review page rebuilt against 4aec322; no diagram changed. Halted for review.
- 2026-09-29 stage 1 approved by the user; score card cached. Next: the design and vocabulary reviews on the complete change, then one pull request.

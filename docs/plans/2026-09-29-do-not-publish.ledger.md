# Ledger, plan: docs/plans/2026-09-29-do-not-publish.md

- 2026-09-29 plan drafted for bilus/documentation-portal#19 on bilus/development, from b7413f3; questions Q1 to Q8, each with an assumption. The endpoints file of conrad-api-docs carries 1 of the 11 markers, on a schema property, for main; the other 10 are in delivery_common.oas2.yml, which the user set aside.
- 2026-09-29 vocabulary: raw spec now leaves out the unpublished parts and the marker keys; added unpublished part and marker key.
- 2026-09-29 design: no diagram change; the raw spec's handler belongs to box 3.3, as decided for #13 (Q5).
- 2026-09-29 holes added: 1 portal.publishedSpec, called by portal.site.rawSpec. Acceptance tests written and skipped: 1 TestPublishedSpec, 1 TestRawSpecLeavesOutUnpublishedParts.
- 2026-09-29 reviews: the design and vocabulary reviews run once, before the pull request (the user's rule).
- 2026-09-29 plan approved by the user. Signatures, top diagram and vocabulary frozen; score card cached.

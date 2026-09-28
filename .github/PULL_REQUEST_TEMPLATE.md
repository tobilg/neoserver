## Summary

<!-- What does this change and why? Link the issue it fixes, if any. -->

## Checklist

- [ ] `make test` and `make vet` pass
- [ ] Bug fixes include a regression test
- [ ] Authorization changes cover anonymous and scoped identities
- [ ] Management API changes: ran `make ui-openapi` and `npm run codegen` (generated clients are not hand-edited)
- [ ] Console changes: `make ui-lint` and `make ui-check` pass
- [ ] Configuration, schema or behavior changes are documented in `docs/` and `docs/release-notes.md`
- [ ] No credentials, `data/` files, build outputs or fixture sessions are committed

See [CONTRIBUTING.md](../CONTRIBUTING.md) for the full workflow.

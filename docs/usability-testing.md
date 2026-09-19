# Usability and accessibility release gate

Automated tests are evidence, not a substitute for unfamiliar users or assistive-technology users. This checklist is the P1 human validation gate. Do not mark it passed from Playwright/axe alone.

## What the automated suite covers

Keyboard interaction, accessible names/roles/states, error and status semantics, responsive behavior and axe checks run automatically in Chromium, Firefox and WebKit.

Human usability and actual screen-reader output are **not covered** by that suite, and a passing run neither passes nor waives this gate. Browser automation does not run VoiceOver/NVDA or establish spoken announcement timing, reading order in browse mode, novice comprehension, real-device usability, or WCAG conformance. Playwright WebKit is also not a qualification of Safari with VoiceOver. The session protocol below is retained for future independent testing; it is not a prerequisite for running the automated suite.

Run the production-build engineering checks without Docker or manual interaction from `web/admin`:

```sh
npm ci
npx playwright install chromium firefox webkit
npm test
npm run build
UI_TEST_BUILD=true npm run test:browser
UI_TEST_BUILD=true npx playwright test --config playwright.ui.config.ts --browser=firefox browser-tests/usability.spec.ts browser-tests/review-fixes.spec.ts
UI_TEST_BUILD=true npx playwright test --config playwright.ui.config.ts --browser=webkit browser-tests/usability.spec.ts browser-tests/review-fixes.spec.ts
```

These browser tests use disposable contract-checked API fixtures, not production credentials or catalogs. Real-backend onboarding and accessibility checks remain separate; use the fixture setup in [development](development.md). Do not infer real-backend publication or authorization success from route mocks.

## Recruit and run

Use at least three developers unfamiliar with neoserver: one API/backend developer, one GIS user and one frontend developer. Include a keyboard-only run and a screen-reader user (VoiceOver/Safari or NVDA/Firefox). Run against the exact release candidate, on disposable catalogs and keys, recording version/commit, OS/browser, theme and viewport. Obtain consent before recordings; exclude token entry, key dialogs and personal/source data from recordings.

Give each participant either the [API tutorial](getting-started.md) or [UI tutorial](getting-started-ui.md), then rotate paths with a fresh workspace. Do not explain internal terms before the task. Ask them to think aloud; record assistance rather than silently guiding them.

| Task | Success evidence | Target |
| --- | --- | --- |
| Start and sign in | Recognizes JWT vs encryption key; reaches empty workspace list | No hidden setup step |
| First publication | Creates workspace, connects PostGIS or uploads sample, publishes private layer | ≤10 minutes after server is ready, no facilitator help |
| See the data | Finds Preview, fits extent, inspects a property | Can explain what was verified |
| Connect a client | Creates viewer key and reads a feature outside console session | ≤5 minutes; no admin credential shared |
| Recover | Fixes wrong database password, disabled service, then restricted layer role | Can identify the relevant fix without reading source |
| Revoke access | Revokes client key, confirms client fails while admin session works | Understands impact and one-time-secret behavior |
| Resume tomorrow | Restarts without reinitializing/deleting data; recovers expired token safely | Existing publication survives |

Record time to first successful request separately from image build/download time. Also capture wrong turns, error wording, terminology confusion, confidence, and any use of JSON when a normal form should suffice. Prioritize task blockers/security misunderstandings as P0; repeated confusion as P1; minor preference as P2.

## Keyboard, screen reader and visual checks

Complete the same journey with keyboard only: visible focus, meaningful tab order, every dialog reachable, Escape/Cancel and focus restoration, validation associated with fields, no keyboard traps. Verify announcements for loading, connection failures, publication results and one-time secrets without exposing them in unrelated live regions. Check native selects, Radix menus, service switches, table actions and copy failure feedback.

In both light and dark themes, test 200% zoom and a 390px viewport. No loss of actions or horizontal page overflow; long code may scroll within its own region. Do not rely on map pixels alone to confirm a data request: Endpoints supplies an explicit status. Geographic visual interpretation still requires the map and should not be represented as screen-reader equivalent functionality.

Automated coverage: `web/admin/e2e/onboarding.spec.ts` creates a fresh workspace through the real UI, publishes a real layer, obtains a viewer key through the UI and requests data in a separate HTTP context; `web/admin/browser-tests/onboarding.spec.ts` covers guide, status/recovery and accessibility states. Existing `e2e/a11y.spec.ts` covers the route matrix. Use WCAG 2.2 AA as the review target, not a certification claim.

## Findings log / sign-off

| Date / candidate | Participant / setup (anonymous) | Task / time | Problem / evidence | Priority / owner | Retested |
| --- | --- | --- | --- | --- | --- |
| 2026-09-16 / working branch | No human sessions; owner declined manual checking | Not measured | Automated engineering checks do not close the human/screen-reader gate | P1 / release owner | Not run |

Release sign-off requires completed human sessions, no unresolved P0 task blockers, explicit disposition of P1 findings, and keyboard/screen-reader evidence attached to the candidate. Failed checks remain findings even if automated tests pass.

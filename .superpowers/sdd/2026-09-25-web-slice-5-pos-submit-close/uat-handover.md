# Web Slice 5 UAT handover

## Preconditions
- Go API running; `bun run scripts/dev-seed.ts` has seeded the catalog.
- A Sales Shift is open.
- Checks run in the web app at the cashier workspace.

## Happy path
1. Add two items, press F9, pay cash. Expect the result screen to show the change and "Đã gửi bếp".
2. Press "Xong", then "Khách tiếp theo" (F9). Expect an empty draft.
3. Press F4. Expect the order listed as "Đang pha chế 0/y món xong".
4. Run `bun run scripts/uat-advance-units.ts <service_number>`.
5. Within 5 s, expect the order to move to the top as "Sẵn sàng hoàn tất", with a green count on the button.
6. Tap it, then press F9 ("Hoàn tất"). Expect the Completed Sale dialog with the correct items, totals, cash tendered, change, and "n món đã giao".
7. Press Enter. Expect the POS to be empty, and the order gone from the drawer.

## Unhappy paths
1. Stop the network after pressing confirm in the payment dialog, once payment is recorded but before submit. Expect "Đã thu tiền nhưng chưa gửi được bếp". Restore the network and press "Gửi bếp (F9)". Expect exactly one Order on the session.
2. Open a session stranded by Slice 4 (listed as "Chờ gửi bếp") from the drawer, then press "Gửi bếp". Expect it to move to "Đang pha chế".
3. In a second tab, open the same order while it is `IN_PREPARATION`. Advance only some units, then force "Hoàn tất" from the first tab after its poll made it ready. Expect the Vietnamese refusal, and the phase to return to `IN_PREPARATION`.
4. Close the same session from two tabs. Expect the second tab to show the same Completed Sale, with no error.
5. Reload during `IN_PREPARATION`. Expect the progress exactly as before.
6. Leave a draft half-built, switch to another order from the drawer, then switch back. Expect the draft unchanged.

## Verification output

### `bun test`
```
bun test v1.4.2 (744846f84)

 262 pass
 0 fail
 626 expect() calls
Ran 262 tests across 32 files. [187.00ms]
```

### `bun run lint`
```
$ oxlint
src/routes/__root.tsx:15:10: warning react(only-export-components): Fast refresh only works when a file only exports components. Move your component(s) to a separate file.
src/routes/_app.tsx:14:10: warning react(only-export-components): Fast refresh only works when a file only exports components. Move your component(s) to a separate file.
src/routes/_app/no-access.tsx:9:10: warning react(only-export-components): Fast refresh only works when a file only exports components. Move your component(s) to a separate file.
src/components/ui/toggle.tsx:42:18: warning react(only-export-components): Fast refresh only works when a file only exports components. Use a new file to share constants or functions between components.
src/components/ui/navigation-menu.tsx:165:3: warning react(only-export-components): Fast refresh only works when a file only exports components. Use a new file to share constants or functions between components.
src/components/ui/tabs.tsx:80:52: warning react(only-export-components): Fast refresh only works when a file only exports components. Use a new file to share constants or functions between components.
src/App.tsx:4:14: warning react(only-export-components): Fast refresh only works when a file only exports components. Use a new file to share constants or functions between components.
src/components/ui/button.tsx:55:18: warning react(only-export-components): Fast refresh only works when a file only exports components. Use a new file to share constants or functions between components.
src/components/ui/badge.tsx:53:17: warning react(only-export-components): Fast refresh only works when a file only exports components. Use a new file to share constants or functions between components.
src/components/ui/combobox.tsx:296:3: warning react(only-export-components): Fast refresh only works when a file only exports components. Use a new file to share constants or functions between components.
```
Exit code: 0. All warnings are pre-existing `react(only-export-components)` warnings in unrelated files (routes, shadcn ui primitives, `App.tsx`); none touch `src/features/pos`.

### `bunx tsc -b`
```
(no output — clean build)
```
Exit code: 0.

### `bun run build`
```
transforming...
✓ 2353 modules transformed.
rendering chunks...
computing gzip size...
dist/index.html                                                   0.78 kB │ gzip:  0.38 kB
...
dist/assets/index-B0eLyLmc.js                                   290.85 kB │ gzip: 92.63 kB

✓ built in 1.74s
```

## Hand-checklist (spec section 11.1)

```
$ grep -rn '"SETTLED"' src/features/pos --include=*.ts --include=*.tsx | grep -v 'state: "SETTLED"'
(no output — no stray SETTLED phase references)

$ wc -l src/features/pos/components/pos-view.tsx
390 src/features/pos/components/pos-view.tsx
```

The only remaining `"SETTLED"` string literals in `src/features/pos` are `state: "SETTLED"` Check-state fixtures in `check-panel.test.tsx`, `pending-orders.test.ts`, and `phase.test.ts`. `pos-view.tsx` is at exactly 390 lines, the plan's hard cap.

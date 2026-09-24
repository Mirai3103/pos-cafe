# Web Slice 6 UAT handover

## Preconditions
- Go API running; `bun run scripts/dev-seed.ts` has seeded the catalog.
- A Sales Shift is open, and at least one Order has been submitted from the POS
  screen (Slice 5's "Gửi bếp") so the queue has active units.
- Checks run in the web app at `/kds`, signed in with a `preparation`
  workspace (or any role carrying `preparation.operate`).

## Happy path
1. Open `/kds`. Expect the submitted order's items grouped into one ticket
   card in the "Chờ pha" column.
2. Press "Bắt đầu làm" on the card. Expect it to move to "Đang pha" within
   5 seconds (or immediately on manual refresh).
3. Press "Xong món". Expect it to move to "Đã xong".
4. Press "Đã giao khách". Expect the ticket to disappear from the board
   entirely (Fulfilled units leave the active queue).
5. In a ticket with two or more items, check only one line's checkbox, then
   press the primary button. Expect only the checked unit to advance,
   leaving the rest of the ticket in its original column.
6. Pick a category filter chip. Expect only units of that category to show;
   "Tất cả" returns the full board.

### Note on very large single tickets (more than 50 units)
A ticket holding more than 50 units still advances with **one tap and one
sound** — the tap is split into consecutive requests of at most 50 unit ids
(the backend's cap), each carrying its own `request_id`. Nothing on screen
asks the operator to repeat the tap; the visible cost is the sequence's own
delay before the card reaches its final column. Two things to expect while
testing a ticket this large:

- **A 5-second poll can catch the sequence mid-flight.** The poll keeps
  running while the batches are sent, so a tick that lands between two
  batches can briefly render the ticket split across two columns (the
  already-advanced units in "Đang pha", the remainder still in "Chờ pha").
  That split is transient: the card settles onto the post-loop invalidation
  as soon as the sequence finishes and the queue is re-read.
- **A rejected batch abandons the batches after it.** Per-unit `FAILED`
  outcomes do not stop the sequence — the affected lines show "Không thể
  chuyển trạng thái", the remaining units keep advancing, and the toast reads
  "n món không thể
  chuyển trạng thái do đã thay đổi. Danh sách đã được cập nhật.". A
  whole-batch rejection (a transport failure, or the batch refused as a
  whole) instead throws out of the loop and drops the batches that have not
  been sent yet, leaving the ticket partly advanced until the operator taps
  again; in that case the board re-reads the queue only on the next 5-second
  poll, because the query is invalidated on success only.

## Waste and Remake
1. On a unit in "Đang pha", press the trash icon. Choose a reason, confirm.
   Expect the unit to leave the board and "Hoạt động gần đây" to show a
   "Huỷ" entry with a "Pha lại" button.
2. Press "Pha lại" on that entry. Expect a new unit to appear in "Chờ pha"
   with a red "PHA LẠI" badge, and the Alerts panel to show the original
   Waste alert until acknowledged.
3. Press "Đã biết" on the alert. Expect it to disappear from the panel.

## Correct-state (Manager Approval)
1. Advance a unit from "Chờ pha" to "Đang pha" by mistake. Press the undo
   icon next to it. Expect the "Hoàn tác thao tác" dialog — the undo icon
   opens that reason-and-note dialog first, it does not jump straight to
   Manager Approval. Pick a reason (and a note when the reason is "Khác"),
   then press "Yêu cầu Quản lý duyệt". Expect the Manager Approval dialog.
2. Enter a Manager's login code and PIN, confirm. Expect the unit to return
   to "Chờ pha".
3. Repeat with a wrong PIN. Expect a Vietnamese "not authorized" message —
   "Mã PIN Quản lý không đúng hoặc không có quyền thực hiện thao tác này." —
   and no state change.

## Unhappy paths
1. Disconnect the network, press a primary action. Expect a Vietnamese error
   toast, and the board unchanged once the network returns and the next
   poll lands.
2. Open `/kds` in two tabs. Advance a unit in one tab. Expect the other tab
   to reflect it within 5 seconds of bringing the other tab into focus,
   without a manual reload — the 5-second poll skips ticks while the document
   is unfocused (`refetchOnWindowFocus` is false), so the update lands on the
   first tick after the tab is focused rather than while it sits in the
   background.
3. Connection loss with active tickets on the board: leave at least one ticket
   sitting in "Chờ pha" (or "Đang pha"), then stop the Go API for about 10–15
   seconds — long enough to cover more than one 5-second poll. Expect:
   - the three columns keep showing their tickets; the board is **not**
     blanked and the page is **not** replaced by the full-screen message
     "Không tải được hàng chờ pha chế. Vui lòng tải lại trang." (that
     full-screen state is reserved for a genuine first load that never
     succeeded);
   - a strip appears above the columns reading "Mất kết nối tới máy chủ. Đang
     hiển thị dữ liệu mới nhất." with a "Thử lại" button.
   Press "Thử lại" while the API is still down. Expect the strip to stay (it
   is not destructive and does not clear the board), and the tiles to remain
   as they were.
   Now restart the Go API and wait for the next poll (up to 5 seconds, no
   manual action required). Expect the strip to disappear on its own and the
   board to pick up any changes made while it was down.

## Verification output

All four commands were run from `web/` at commit
`8dc0512` (task baseline) with no source changes.

### `bun test`
Per-test `(pass)` lines are omitted below; the counts and summary are verbatim.

```
bun test v1.4.2 (744846f84)

[per-test (pass) lines omitted — 320 of them]

 320 pass
 0 fail
 768 expect() calls
Ran 320 tests across 43 files. [329.00ms]
```
Exit code: 0. 58 of the 320 tests cover `src/features/kds` (11 files).

### `bun run lint`
```
$ oxlint
src/routes/__root.tsx:15:10: warning react(only-export-components): Fast refresh only works when a file only exports components. Move your component(s) to a separate file.
src/App.tsx:4:14: warning react(only-export-components): Fast refresh only works when a file only exports components. Use a new file to share constants or functions between components.
src/components/ui/toggle.tsx:42:18: warning react(only-export-components): Fast refresh only works when a file only exports components. Use a new file to share constants or functions between components.
src/components/ui/tabs.tsx:80:52: warning react(only-export-components): Fast refresh only works when a file only exports components. Use a new file to share constants or functions between components.
src/routes/_app.tsx:14:10: warning react(only-export-components): Fast refresh only works when a file only exports components. Move your component(s) to a separate file.
src/routes/_app/no-access.tsx:9:10: warning react(only-export-components): Fast refresh only works when a file only exports components. Move your component(s) to a separate file.
src/components/ui/combobox.tsx:296:3: warning react(only-export-components): Fast refresh only works when a file only exports components. Use a new file to share constants or functions between components.
src/components/ui/navigation-menu.tsx:165:3: warning react(only-export-components): Fast refresh only works when a file only exports components. Use a new file to share constants or functions between components.
src/components/ui/button.tsx:55:18: warning react(only-export-components): Fast refresh only works when a file only exports components. Use a new file to share constants or functions between components.
src/components/ui/badge.tsx:53:17: warning react(only-export-components): Fast refresh only works when a file only exports components. Use a new file to share constants or functions between components.
```
Exit code: 0. All 10 warnings are the same pre-existing
`react(only-export-components)` warnings recorded in Slice 5's handover
(identical file list, only the print order differs); every one is in
`src/routes`, `src/App.tsx`, or a shadcn `src/components/ui` primitive. None
is in `src/features/kds`, so none is KDS-related.

### `bunx tsc -b`
```
(no output — clean build)
```
Exit code: 0.

### `bun run build`
ANSI colour codes stripped from the captured output; text otherwise verbatim.

```
$ tsc -b && vite build
vite v8.3.0 building client environment for production...
transforming...
✓ 2397 modules transformed.
rendering chunks...
computing gzip size...
dist/index.html                                                   0.78 kB │ gzip:  0.37 kB
dist/assets/jetbrains-mono-greek-400-normal-C190GLew.woff2        4.22 kB
dist/assets/jetbrains-mono-cyrillic-400-normal-BEIGL1Tu.woff2     5.32 kB
dist/assets/jetbrains-mono-vietnamese-400-normal-CqNFfHCs.woff    5.37 kB
dist/assets/jetbrains-mono-greek-400-normal-B9oWc5Lo.woff         5.66 kB
dist/assets/jetbrains-mono-cyrillic-400-normal-ugxPyKxw.woff      6.97 kB
dist/assets/jetbrains-mono-latin-ext-400-normal-Bc8Ftmh3.woff2    7.33 kB
dist/assets/jetbrains-mono-latin-ext-400-normal-fXTG6kC5.woff    10.12 kB
dist/assets/outfit-latin-ext-wght-normal-DdQaqQDo.woff2          14.80 kB
dist/assets/jetbrains-mono-latin-400-normal-V6pRDFza.woff2       21.16 kB
dist/assets/jetbrains-mono-latin-400-normal-6-qcROiO.woff        27.49 kB
dist/assets/outfit-latin-wght-normal-Bc-8i84L.woff2              32.29 kB
dist/assets/index-whylxYKm.css                                  154.41 kB │ gzip: 29.99 kB
dist/assets/x-Bg6yZwkr.js                                         0.18 kB │ gzip:  0.16 kB
dist/assets/clock-0X0gk8Br.js                                     0.19 kB │ gzip:  0.18 kB
dist/assets/search-CHtg6Qi4.js                                    0.20 kB │ gzip:  0.18 kB
dist/assets/rotate-ccw-lMO2gH8K.js                                0.22 kB │ gzip:  0.20 kB
dist/assets/lock-D8NasXUw.js                                      0.23 kB │ gzip:  0.21 kB
dist/assets/grid-2x2-CSsZNBrR.js                                  0.27 kB │ gzip:  0.21 kB
dist/assets/circle-alert-CQohzAvu.js                              0.30 kB │ gzip:  0.21 kB
dist/assets/coffee-BPNt0K2_.js                                    0.33 kB │ gzip:  0.22 kB
dist/assets/chef-hat-BEdykABJ.js                                  0.33 kB │ gzip:  0.25 kB
dist/assets/printer-BRTdtu4z.js                                   0.34 kB │ gzip:  0.25 kB
dist/assets/refresh-cw-CQpeRCrP.js                                0.34 kB │ gzip:  0.24 kB
dist/assets/utils-Bq3jW8zj.js                                     0.35 kB │ gzip:  0.24 kB
dist/assets/plus-3I5UqVgF.js                                      0.36 kB │ gzip:  0.25 kB
dist/assets/use-manager-approval-store-uGXoe1TQ.js                0.49 kB │ gzip:  0.28 kB
dist/assets/settings-yI8qjCPk.js                                  0.51 kB │ gzip:  0.27 kB
dist/assets/guards-CBD6BWUT.js                                    0.54 kB │ gzip:  0.35 kB
dist/assets/receipt-AlrTk42h.js                                   0.68 kB │ gzip:  0.30 kB
dist/assets/shopping-cart-DLtdbSS8.js                             0.70 kB │ gzip:  0.42 kB
dist/assets/rolldown-runtime-hePW80VL.js                          0.71 kB │ gzip:  0.42 kB
dist/assets/volume-x-KykDR6oP.js                                  0.74 kB │ gzip:  0.35 kB
dist/assets/no-access-DgiJ6spu.js                                 1.34 kB │ gzip:  0.80 kB
dist/assets/badge-BRsUw1KK.js                                     1.51 kB │ gzip:  0.69 kB
dist/assets/history-BnR_mKo6.js                                   1.68 kB │ gzip:  0.97 kB
dist/assets/createBaseUIEventDetails-B21Yt8IA.js                  1.69 kB │ gzip:  0.88 kB
dist/assets/settings-Cn3c5wlr.js                                  1.94 kB │ gzip:  1.07 kB
dist/assets/placeholder-page-oaWgIsWA.js                          2.07 kB │ gzip:  0.80 kB
dist/assets/card-CLJojWdy.js                                      2.48 kB │ gzip:  0.76 kB
dist/assets/tables-D_5CF8rP.js                                    2.54 kB │ gzip:  1.18 kB
dist/assets/auth-header-C8uRwg4M.js                               3.29 kB │ gzip:  1.33 kB
dist/assets/pin-pad-6BSxusls.js                                   3.73 kB │ gzip:  1.25 kB
dist/assets/visuallyHidden-kTc52Ngb.js                            3.91 kB │ gzip:  1.77 kB
dist/assets/input-UtqI1rO0.js                                     5.30 kB │ gzip:  2.38 kB
dist/assets/workspace-BNfXrsaC.js                                 5.55 kB │ gzip:  2.37 kB
dist/assets/login-CeQcclvJ.js                                     6.68 kB │ gzip:  2.84 kB
dist/assets/use-shift-mhryMNir.js                                 6.92 kB │ gzip:  1.67 kB
dist/assets/use-keypad-hotkeys-Fxk40N33.js                        7.10 kB │ gzip:  2.75 kB
dist/assets/error-messages-D9JmZRO2.js                            7.44 kB │ gzip:  2.88 kB
dist/assets/command-QKlGGPEh.js                                   8.63 kB │ gzip:  3.31 kB
dist/assets/button-CWn6-OL8.js                                   11.57 kB │ gzip:  4.61 kB
dist/assets/_app-CqyXmfQb.js                                     17.62 kB │ gzip:  6.63 kB
dist/assets/preload-helper-FJaxyooF.js                           24.89 kB │ gzip:  9.52 kB
dist/assets/dist-DZSHa31Y.js                                     28.30 kB │ gzip: 11.80 kB
dist/assets/shift-Bs2z7Ypo.js                                    49.30 kB │ gzip: 13.53 kB
dist/assets/kds-CtcwSLIj.js                                      52.97 kB │ gzip: 17.91 kB
dist/assets/use-auth-BSuI7ZmG.js                                 68.28 kB │ gzip: 24.53 kB
dist/assets/_app-CpjgV9fV.js                                    171.38 kB │ gzip: 50.76 kB
dist/assets/index-RBzFFj-v.js                                   291.09 kB │ gzip: 92.76 kB

✓ built in 1.87s
```
Exit code: 0. No build warnings.

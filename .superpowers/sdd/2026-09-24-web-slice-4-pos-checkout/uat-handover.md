# Web Slice 4 UAT Handover: POS-b Commit, Check & Cash Payment

## Setup

1. `go run ./cmd/api` with a clean database.
2. `cd web && bun run scripts/dev-seed.ts` to seed the Manager and the menu.
3. `cd web && bun run dev`, sign in as `QL01`.
4. Open a Sales Shift from `/shift` with a starting float.

## Happy path

1. On the POS screen, add three items, at least one with a size and a topping.
2. Confirm the bill total.
3. Press **F9**.
4. Tap the **Đúng tiền** button. The change reads 0.
5. Clear the field and type an amount above the total. The change updates as you type.
6. Press **Enter**. The dialog shows **Tiền thối** with the change to hand back.
7. Press **Enter** again. The bill panel reads **Đã thanh toán**.
8. Press **F9**. A fresh empty bill appears for the next customer.

## Checks to make

- The change on the result screen matches the change shown while typing.
- The amount charged matches the bill total shown before F9.
- `Gửi bếp` is visible but disabled, captioned `Mở ở Slice 5`.
- The `VietQR` tab is visible but disabled.

## Unhappy paths

1. **Reload mid-payment.** Add items, press F9, confirm, and immediately reload
   the page while the request is in flight. The bill must come back either as an
   editable draft or as a committed Check showing the outstanding balance — never
   blank, and never as an editable draft with a Check already charged.
2. **An item goes away.** With items in the draft, open Admin in a second tab and
   mark one of them unavailable. Press F9 and confirm. The payment is refused, a
   Vietnamese message names the problem, and the draft is still editable.
3. **Network failure, then retry.** Add items, press F9, stop the Go server,
   confirm, restart the server, and press the confirm button again. Then open
   `/shift` and verify that exactly one cash payment was recorded for the amount.

## Known and accepted

- A settled Session stays `ACTIVE` and cannot be closed: Submit arrives in
  Slice 5. Taking the next customer opens a new Session rather than reusing this
  one. Recorded in the design spec, section 2.
